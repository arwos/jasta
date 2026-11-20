package config

type Validater interface {
	Validate() error
}

type Rooter interface {
	GetRoot() string
	SetRoot(root string)
}
