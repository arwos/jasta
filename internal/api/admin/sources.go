package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/web"

	"go.arwos.org/jasta/internal/pkg/archive"
)

type sourceParams struct {
	ID          string           `json:"id,omitempty"`
	Name        string           `json:"name"`
	URL         string           `json:"url"`
	AuthType    archive.AuthType `json:"auth_type"`
	Username    string           `json:"username,omitempty"`
	Password    string           `json:"password,omitempty"`
	BearerToken string           `json:"bearer_token,omitempty"`
}

type storedSourceCredentials struct {
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	BearerToken string `json:"bearer_token,omitempty"`
}

func (a *Api) createSource(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params sourceParams
	if decodeParams(raw, &params) != nil || params.ID != "" {
		return nil, invalidParams()
	}
	return a.saveSource(ctx, params, false)
}

func (a *Api) updateSource(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params sourceParams
	if decodeParams(raw, &params) != nil || params.ID == "" {
		return nil, invalidParams()
	}
	if _, err := uuid.Parse(params.ID); err != nil {
		return nil, invalidParams()
	}
	return a.saveSource(ctx, params, true)
}

func (a *Api) saveSource(ctx context.Context, params sourceParams, update bool) (any, error) {
	params.Name = strings.TrimSpace(params.Name)
	if len(params.Name) == 0 || len(params.Name) > 128 {
		return nil, invalidParams()
	}
	if params.AuthType != archive.AuthNone && params.AuthType != archive.AuthBasic &&
		params.AuthType != archive.AuthBearer && params.AuthType != archive.AuthQuery {
		return nil, invalidParams()
	}
	source := archive.Source{
		URL: params.URL, AuthType: params.AuthType, Username: params.Username,
		Password: params.Password, BearerToken: params.BearerToken,
	}
	if err := archive.ValidateSource(source); err != nil {
		return nil, invalidParams()
	}
	parsedURL, err := url.Parse(params.URL)
	if err != nil {
		return nil, invalidParams()
	}
	query := parsedURL.RawQuery
	parsedURL.RawQuery = ""
	baseURL := parsedURL.String()
	var queryCiphertext, credentialsCiphertext []byte
	if query != "" {
		queryCiphertext, err = a.secretCodec.Encrypt([]byte(query))
		if err != nil {
			return nil, internalError()
		}
	}
	credentials := storedSourceCredentials{Username: params.Username, Password: params.Password, BearerToken: params.BearerToken}
	if params.AuthType != archive.AuthNone && params.AuthType != archive.AuthQuery {
		encoded, marshalErr := json.Marshal(credentials)
		if marshalErr != nil {
			return nil, internalError()
		}
		credentialsCiphertext, err = a.secretCodec.Encrypt(encoded)
		if err != nil {
			return nil, internalError()
		}
	}

	id := params.ID
	if !update {
		generated, generateErr := uuid.NewRandom()
		if generateErr != nil {
			return nil, internalError()
		}
		id = generated.String()
	}
	now := time.Now().Unix()
	err = a.database.Tag("master").CallContext(ctx, "save registry source", func(ctx context.Context, db orm.DB) error {
		if update {
			result, updateErr := db.ExecContext(ctx, `UPDATE registry_sources SET name = ?, url = ?, url_query_ciphertext = ?,
				auth_type = ?, credentials_ciphertext = ?, updated_at = ? WHERE id = ?`,
				params.Name, baseURL, queryCiphertext, params.AuthType, nullableBytes(credentialsCiphertext), now, id)
			if updateErr != nil {
				return updateErr
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return countErr
			}
			if count != 1 {
				return sql.ErrNoRows
			}
			return nil
		}
		_, insertErr := db.ExecContext(ctx, `INSERT INTO registry_sources
			(id, name, url, url_query_ciphertext, auth_type, credentials_ciphertext, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, params.Name, baseURL, queryCiphertext, params.AuthType,
			nullableBytes(credentialsCiphertext), now, now)
		return insertErr
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &rpcError{code: -32004, message: "source not found"}
	}
	if err != nil {
		return nil, &rpcError{code: -32009, message: "source name already exists or source could not be saved"}
	}
	return map[string]string{"id": id, "name": params.Name, "auth_type": string(params.AuthType)}, nil
}

func (a *Api) deleteSource(ctx context.Context, _ web.Ctx, raw json.RawMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if decodeParams(raw, &params) != nil || params.ID == "" {
		return nil, invalidParams()
	}
	if _, err := uuid.Parse(params.ID); err != nil {
		return nil, invalidParams()
	}
	err := a.database.Tag("master").CallContext(ctx, "delete registry source", func(ctx context.Context, db orm.DB) error {
		result, err := db.ExecContext(ctx, "DELETE FROM registry_sources WHERE id = ?", params.ID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &rpcError{code: -32004, message: "source not found"}
	}
	if err != nil {
		return nil, internalError()
	}
	return map[string]bool{"ok": true}, nil
}

// LoadSource decrypts registry material only for an in-memory archive fetch.
// Callers must not log or serialize the returned value.
func (a *Api) LoadSource(ctx context.Context, sourceID string) (archive.Source, error) {
	var rawURL string
	var rawQuery, rawCredentials []byte
	var authType archive.AuthType
	err := a.database.Tag("master").CallContext(ctx, "load registry source for fetch", func(ctx context.Context, db orm.DB) error {
		queryer, ok := db.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return errors.New("database connection does not support row queries")
		}
		return queryer.QueryRowContext(ctx, `SELECT url, url_query_ciphertext, auth_type, credentials_ciphertext
			FROM registry_sources WHERE id = ?`, sourceID).Scan(&rawURL, &rawQuery, &authType, &rawCredentials)
	})
	if err != nil {
		return archive.Source{}, fmt.Errorf("load registry source: %w", err)
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return archive.Source{}, errors.New("stored registry URL is invalid")
	}
	if len(rawQuery) > 0 {
		query, decryptErr := a.secretCodec.Decrypt(rawQuery)
		if decryptErr != nil {
			return archive.Source{}, errors.New("stored registry query could not be decrypted")
		}
		parsedURL.RawQuery = string(query)
	}
	source := archive.Source{URL: parsedURL.String(), AuthType: authType}
	if len(rawCredentials) > 0 {
		plain, decryptErr := a.secretCodec.Decrypt(rawCredentials)
		if decryptErr != nil {
			return archive.Source{}, errors.New("stored registry credentials could not be decrypted")
		}
		var credentials storedSourceCredentials
		if unmarshalErr := json.Unmarshal(plain, &credentials); unmarshalErr != nil {
			return archive.Source{}, errors.New("stored registry credentials are invalid")
		}
		source.Username, source.Password, source.BearerToken = credentials.Username, credentials.Password, credentials.BearerToken
	}
	if err = archive.ValidateSource(source); err != nil {
		return archive.Source{}, errors.New("stored registry source is invalid")
	}
	return source, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
