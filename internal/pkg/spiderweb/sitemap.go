package spiderweb

import (
	"bytes"
	"fmt"
	"os"
	"time"
)

func generateSitemap(links []string, domain, filepath string) error {
	date := time.Now().Format("2006-01-02")

	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	buf.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")

	for _, link := range links {
		buf.WriteString(fmt.Sprintf("<url>"+
			"<loc>%s%s</loc>"+
			"<changefreq>daily</changefreq>"+
			"<priority>0.7</priority>"+
			"<lastmod>%s</lastmod></url>\n", domain, link, date))
	}

	buf.WriteString("</urlset>\n")

	return os.WriteFile(filepath, buf.Bytes(), 0755)
}
