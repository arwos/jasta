package spiderweb

const configName = ".jasta.yaml"

type Config struct {
	DevHost string `yaml:"dev_host"`
	OutDir  string `yaml:"out_dir"`
	Sitemap string `yaml:"sitemap"`
	Domain  string `yaml:"domain"`
}
