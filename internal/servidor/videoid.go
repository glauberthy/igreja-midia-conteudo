package servidor

import (
	"net/url"
	"strings"
)

// videoID extrai o id do vídeo de uma URL do YouTube, cobrindo as formas comuns:
//
//	https://www.youtube.com/watch?v=ID        (query v)
//	https://youtu.be/ID                        (path curto)
//	https://www.youtube.com/embed/ID           (embed)
//	https://www.youtube.com/shorts/ID          (shorts)
//	https://m.youtube.com/watch?v=ID           (mobile)
//
// Parâmetros extras (&t=, &list=) são ignorados. Devolve "" se não reconhecer.
func videoID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")

	if host == "youtu.be" {
		return primeiroSegmento(u.Path)
	}
	if host == "youtube.com" || host == "m.youtube.com" || strings.HasSuffix(host, ".youtube.com") {
		if v := u.Query().Get("v"); v != "" {
			return v
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "embed", "v", "shorts", "live":
				return parts[1]
			}
		}
	}
	return ""
}

// primeiroSegmento devolve o primeiro trecho não vazio do path (sem as barras).
func primeiroSegmento(path string) string {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg != "" {
			return seg
		}
	}
	return ""
}
