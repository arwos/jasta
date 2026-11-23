package application

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.osspkg.com/markdown"
	"gopkg.in/yaml.v3"

	"go.arwos.org/jasta/internal/pkg/config"
)

const (
	markdownFileLimit = 16 << 20
	markdownSiteLimit = 1 << 30
	markdownMaxDepth  = 128
	markdownMaxTokens = 1_000_000
	templateFileLimit = 1 << 20
	templateSetLimit  = 16 << 20
	templateDir       = "templates"
	defaultTemplate   = "page.html"
)

type markdownFrontMatter struct {
	Slug        string `yaml:"slug"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Template    string `yaml:"template"`
}

type markdownTemplateData struct {
	Slug        string
	Title       string
	Description string
	Content     template.HTML
}

type markdownInputFile struct {
	name string
	info fs.FileInfo
}

type markdownSourceFiles struct {
	files        []markdownInputFile
	templates    []string
	totalBytes   int64
	templateSize int64
}

type markdownPage struct {
	output string
	data   markdownTemplateData
	tmpl   string
}

// BuildMarkdownSite compiles Markdown sources into the configured output root.
// Admin/deployment code calls it before activating a release; the web runtime
// only serves the generated files.
func BuildMarkdownSite(conf *config.MarkdownApplication) error {
	if conf == nil {
		return errors.New("markdown config is required")
	}
	if err := conf.Validate(); err != nil {
		return err
	}
	if conf.Source == "" {
		return errors.New("markdown source is required for a build")
	}
	return buildMarkdownSite(conf)
}

func buildMarkdownSite(conf *config.MarkdownApplication) (err error) {
	sourcePath, outputPath, indexName, err := markdownBuildPaths(conf)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(sourcePath)
	if err != nil {
		return fmt.Errorf("open markdown source directory %q: %w", sourcePath, err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close markdown source directory: %w", closeErr)
		}
	}()

	sourceFiles, err := scanMarkdownFiles(source)
	if err != nil {
		return err
	}
	templates, err := parseMarkdownTemplates(source, sourceFiles.templates)
	if err != nil {
		return err
	}
	if templates.Lookup(defaultTemplate) == nil {
		return fmt.Errorf("markdown template %q is required in %s/", defaultTemplate, templateDir)
	}
	converter, err := markdown.New(
		markdown.WithMaxInputBytes(markdownFileLimit),
		markdown.WithMaxNestingDepth(markdownMaxDepth),
		markdown.WithMaxTokens(markdownMaxTokens),
	)
	if err != nil {
		return fmt.Errorf("configure markdown converter: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(outputPath), ".jasta-markdown-build-*")
	if err != nil {
		return fmt.Errorf("create markdown build staging directory: %w", err)
	}
	defer func() {
		if stage != "" {
			_ = os.RemoveAll(stage)
		}
	}()
	if err = copyMarkdownAssets(source, stage, sourceFiles.files); err != nil {
		return err
	}
	pageCount, err := renderMarkdownPages(source, stage, sourceFiles.files, templates, converter, indexName)
	if err != nil {
		return err
	}
	if pageCount == 0 {
		return errors.New("markdown source does not contain any .md or .markdown pages")
	}
	if err = replaceMarkdownOutput(stage, outputPath); err != nil {
		return err
	}
	stage = ""
	return nil
}

func markdownBuildPaths(conf *config.MarkdownApplication) (string, string, string, error) {
	sourcePath, err := filepath.Abs(conf.Source)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve markdown source %q: %w", conf.Source, err)
	}
	outputPath, err := filepath.Abs(conf.Root)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve markdown output %q: %w", conf.Root, err)
	}
	if pathsOverlap(sourcePath, outputPath) {
		return "", "", "", fmt.Errorf("markdown source %q and output %q must be separate directories", sourcePath, outputPath)
	}
	outputParent := filepath.Dir(outputPath)
	if err = os.MkdirAll(outputParent, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create markdown output parent %q: %w", outputParent, err)
	}
	outputParent, err = filepath.EvalSymlinks(outputParent)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve markdown output parent: %w", err)
	}
	outputPath = filepath.Join(outputParent, filepath.Base(outputPath))
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve markdown source directory %q: %w", conf.Source, err)
	}
	if pathsOverlap(sourcePath, outputPath) {
		return "", "", "", fmt.Errorf("markdown source %q and output %q must be separate directories", sourcePath, outputPath)
	}
	indexName, err := cleanContentPath(conf.Pages.Index)
	if err != nil || indexName == "" || filepath.IsAbs(indexName) {
		return "", "", "", fmt.Errorf("invalid markdown index filename %q", conf.Pages.Index)
	}
	return sourcePath, outputPath, indexName, nil
}

func scanMarkdownFiles(source *os.Root) (markdownSourceFiles, error) {
	files := markdownSourceFiles{}
	err := walkMarkdownSource(source, ".", func(name string, entry fs.DirEntry) error {
		return files.add(name, entry)
	})
	if err != nil {
		return markdownSourceFiles{}, fmt.Errorf("scan markdown source: %w", err)
	}
	return files, nil
}

func (files *markdownSourceFiles) add(name string, entry fs.DirEntry) error {
	if entry.Type()&fs.ModeSymlink != 0 {
		return fmt.Errorf("symbolic links are not allowed in markdown source: %q", name)
	}
	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("inspect markdown source file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("non-regular files are not allowed in markdown source: %q", name)
	}
	if strings.HasPrefix(name, templateDir+"/") {
		if path.Dir(name) != templateDir {
			return fmt.Errorf("markdown templates must be directly inside %s/: %q", templateDir, name)
		}
		if strings.ToLower(filepath.Ext(name)) != ".html" {
			return nil
		}
		if info.Size() > templateFileLimit {
			return fmt.Errorf("template %q exceeds %d bytes", name, templateFileLimit)
		}
		files.templates = append(files.templates, name)
		files.totalBytes += info.Size()
		files.templateSize += info.Size()
		if files.totalBytes > markdownSiteLimit || files.templateSize > templateSetLimit {
			return errors.New("markdown source exceeds configured build limits")
		}
		return nil
	}
	if isMarkdownFile(name) && info.Size() > markdownFileLimit {
		return fmt.Errorf("markdown file %q exceeds %d bytes", name, markdownFileLimit)
	}
	if info.Size() > markdownSiteLimit-files.totalBytes {
		return fmt.Errorf("markdown source exceeds %d bytes", markdownSiteLimit)
	}
	files.totalBytes += info.Size()
	files.files = append(files.files, markdownInputFile{name: name, info: info})
	return nil
}

func renderMarkdownPages(source *os.Root, stage string, files []markdownInputFile, templates *template.Template, converter *markdown.Converter, indexName string) (int, error) {
	staticFiles := make(map[string]string, len(files))
	for _, file := range files {
		if !isMarkdownFile(file.name) {
			staticFiles[filepath.ToSlash(file.name)] = file.name
		}
	}
	pageOutputs := make(map[string]string)
	pageCount := 0
	for _, file := range files {
		if !isMarkdownFile(file.name) {
			continue
		}
		page, err := compileMarkdownPage(source, file, templates, converter, indexName)
		if err != nil {
			return 0, err
		}
		if previous, exists := pageOutputs[page.output]; exists {
			return 0, fmt.Errorf("markdown files %q and %q produce the same page %q", previous, file.name, page.data.Slug)
		}
		if staticFile, exists := staticFiles[page.output]; exists {
			return 0, fmt.Errorf("generated markdown page %q conflicts with static file %q", page.data.Slug, staticFile)
		}
		pageOutputs[page.output] = file.name
		if err := writeMarkdownPage(stage, page, templates); err != nil {
			return 0, err
		}
		pageCount++
	}
	return pageCount, nil
}

func compileMarkdownPage(source *os.Root, file markdownInputFile, templates *template.Template, converter *markdown.Converter, indexName string) (markdownPage, error) {
	data, err := readMarkdownFile(source, file.name)
	if err != nil {
		return markdownPage{}, err
	}
	frontMatter, body, err := parseMarkdownFrontMatter(file.name, data)
	if err != nil {
		return markdownPage{}, err
	}
	slug, err := normalizeMarkdownSlug(frontMatter.Slug)
	if err != nil {
		return markdownPage{}, fmt.Errorf("markdown file %q has invalid slug: %w", file.name, err)
	}
	templateName := frontMatter.Template
	if templateName == "" {
		templateName = defaultTemplate
	}
	if templateName != filepath.Base(templateName) || !strings.HasSuffix(templateName, ".html") || templates.Lookup(templateName) == nil {
		return markdownPage{}, fmt.Errorf("markdown file %q selects unknown template %q", file.name, templateName)
	}
	converted, err := converter.Convert(string(body))
	if err != nil {
		return markdownPage{}, fmt.Errorf("convert markdown file %q: %w", file.name, err)
	}
	return markdownPage{
		output: markdownSlugOutput(slug, indexName),
		tmpl:   templateName,
		data: markdownTemplateData{
			Slug:        slug,
			Title:       frontMatter.Title,
			Description: frontMatter.Description,
			Content:     template.HTML(converted.HTML), // #nosec G203 -- go-markdown escapes raw HTML and unsafe destinations by default.
		},
	}, nil
}

func pathsOverlap(first, second string) bool {
	rel, err := filepath.Rel(first, second)
	if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return true
	}
	rel, err = filepath.Rel(second, first)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func isMarkdownFile(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".md" || extension == ".markdown"
}

func walkMarkdownSource(root *os.Root, directory string, visit func(string, fs.DirEntry) error) error {
	file, err := root.Open(directory)
	if err != nil {
		return err
	}
	entries, err := file.ReadDir(-1)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		if directory != "." {
			name = path.Join(directory, name)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed in Markdown source: %q", name)
		}
		if entry.IsDir() {
			if err := walkMarkdownSource(root, name, visit); err != nil {
				return err
			}
			continue
		}
		if err := visit(name, entry); err != nil {
			return err
		}
	}
	return nil
}

func parseMarkdownTemplates(source *os.Root, files []string) (*template.Template, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no .html templates found in %s/", templateDir)
	}
	tmpl := template.New("pages")
	var total int64
	for _, name := range files {
		if path.Dir(name) != templateDir {
			return nil, fmt.Errorf("markdown templates must be directly inside %s/: %q", templateDir, name)
		}
		data, err := readBoundedFile(source, name, templateFileLimit)
		if err != nil {
			return nil, err
		}
		total += int64(len(data))
		if total > templateSetLimit {
			return nil, fmt.Errorf("markdown templates exceed %d bytes", templateSetLimit)
		}
		if _, err := tmpl.New(path.Base(name)).Parse(string(data)); err != nil {
			return nil, fmt.Errorf("parse Markdown template %q: %w", name, err)
		}
	}
	return tmpl, nil
}

func readMarkdownFile(root *os.Root, name string) ([]byte, error) {
	return readBoundedFile(root, name, markdownFileLimit)
}

func readBoundedFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open markdown file %q: %w", name, err)
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read markdown file %q: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file %q exceeds %d bytes", name, limit)
	}
	return data, nil
}

func parseMarkdownFrontMatter(name string, data []byte) (markdownFrontMatter, []byte, error) {
	var frontMatter markdownFrontMatter
	lines := bytes.SplitAfter(data, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return frontMatter, nil, fmt.Errorf("markdown file %q must start with YAML front matter", name)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) != "---" {
			continue
		}
		yamlData := bytes.Join(lines[1:i], nil)
		decoder := yaml.NewDecoder(bytes.NewReader(yamlData))
		decoder.KnownFields(true)
		if err := decoder.Decode(&frontMatter); err != nil {
			return frontMatter, nil, fmt.Errorf("decode front matter in %q: %w", name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple YAML documents are not allowed")
			}
			return frontMatter, nil, fmt.Errorf("decode front matter in %q: %w", name, err)
		}
		body := bytes.Join(lines[i+1:], nil)
		if frontMatter.Slug == "" {
			return frontMatter, nil, fmt.Errorf("markdown file %q is missing required front matter slug", name)
		}
		return frontMatter, body, nil
	}
	return frontMatter, nil, fmt.Errorf("markdown file %q has unterminated YAML front matter", name)
}

func normalizeMarkdownSlug(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\\') {
		return "", fmt.Errorf("slug must be a root-relative URL path")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("slug must contain only a URL path")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." || strings.ContainsRune(segment, 0) {
			return "", fmt.Errorf("slug cannot contain dot or traversal segments")
		}
	}
	return path.Clean(parsed.Path), nil
}

func markdownSlugOutput(slug, indexName string) string {
	relative := strings.TrimPrefix(slug, "/")
	if relative == "" {
		return indexName
	}
	return path.Join(relative, indexName)
}

func copyMarkdownAssets(source *os.Root, stage string, files []markdownInputFile) error {
	for _, file := range files {
		if isMarkdownFile(file.name) {
			continue
		}
		destination := filepath.Join(stage, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create static asset directory for %q: %w", file.name, err)
		}
		input, err := source.Open(file.name)
		if err != nil {
			return fmt.Errorf("open static asset %q: %w", file.name, err)
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("create static asset %q: %w", file.name, err)
		}
		copied, copyErr := io.Copy(output, io.LimitReader(input, file.info.Size()+1))
		inputErr := input.Close()
		outputErr := output.Close()
		if copyErr != nil {
			return fmt.Errorf("copy static asset %q: %w", file.name, copyErr)
		}
		if inputErr != nil {
			return fmt.Errorf("close static asset %q: %w", file.name, inputErr)
		}
		if outputErr != nil {
			return fmt.Errorf("close generated static asset %q: %w", file.name, outputErr)
		}
		if copied != file.info.Size() {
			return fmt.Errorf("static asset %q changed while building: copied %d bytes, expected %d", file.name, copied, file.info.Size())
		}
	}
	return nil
}

func writeMarkdownPage(stage string, page markdownPage, templates *template.Template) error {
	destination := filepath.Join(stage, filepath.FromSlash(page.output))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create generated page directory for %q: %w", page.data.Slug, err)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) // #nosec G302 -- generated public pages must be readable by the web server.
	if err != nil {
		return fmt.Errorf("create generated Markdown page %q: %w", page.data.Slug, err)
	}
	executeErr := templates.ExecuteTemplate(file, page.tmpl, page.data)
	closeErr := file.Close()
	if executeErr != nil {
		return fmt.Errorf("render Markdown page %q: %w", page.data.Slug, executeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close generated Markdown page %q: %w", page.data.Slug, closeErr)
	}
	return nil
}

func replaceMarkdownOutput(stage, output string) error {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create Markdown output parent %q: %w", parent, err)
	}
	backup := ""
	if info, err := os.Lstat(output); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("markdown output %q must be a real directory", output)
		}
		backup, err = os.MkdirTemp(parent, ".jasta-markdown-backup-*")
		if err != nil {
			return fmt.Errorf("reserve Markdown output backup: %w", err)
		}
		if err = os.Remove(backup); err != nil {
			return fmt.Errorf("prepare Markdown output backup: %w", err)
		}
		if err = os.Rename(output, backup); err != nil {
			return fmt.Errorf("move previous Markdown output aside: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Markdown output %q: %w", output, err)
	}
	if err := os.Rename(stage, output); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return errors.Join(
					fmt.Errorf("activate markdown output: %w", err),
					fmt.Errorf("restore previous output: %w", restoreErr),
				)
			}
		}
		return fmt.Errorf("activate markdown output: %w", err)
	}
	// Keep the prior generated tree. Release retention is managed separately;
	// rebuilding a Markdown site must not silently discard existing content.
	return nil
}
