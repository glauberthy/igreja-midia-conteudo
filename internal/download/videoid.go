package download

import (
	"net/url"
	"strings"
)

// VideoID extrai o id do vídeo de uma URL do YouTube, cobrindo as formas reais:
//
//	https://www.youtube.com/watch?v=ID        (query v)
//	https://youtu.be/ID                        (path curto)
//	https://www.youtube.com/live/ID            (TRANSMISSÃO AO VIVO — o caso desta igreja)
//	https://www.youtube.com/embed/ID           (embed)
//	https://www.youtube.com/shorts/ID          (shorts)
//	https://m.youtube.com/watch?v=ID           (mobile)
//
// Parâmetros extras (&t=, &list=, ?feature=share) são ignorados. Devolve "" se não
// reconhecer OU se o id não passar na validação de formato (ver idValido).
//
// Mudou de lugar (era internal/servidor) e ganhou validação porque o uso mudou de natureza:
// antes o id só alimentava um iframe; agora ele é NOME DE DIRETÓRIO no cache
// (videos/<id>/). Sem validar, uma URL hostil escolheria onde escrevemos — a mesma
// preocupação de retencao.caminhoSeguro, num lugar novo.
func VideoID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")

	var id string
	switch {
	case host == "youtu.be":
		id = primeiroSegmento(u.Path)
	case host == "youtube.com" || host == "m.youtube.com" ||
		host == "music.youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		if v := u.Query().Get("v"); v != "" {
			id = v
		} else {
			partes := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(partes) >= 2 {
				switch partes[0] {
				case "embed", "v", "shorts", "live":
					id = partes[1]
				}
			}
		}
	}
	if !idValido(id) {
		return ""
	}
	return id
}

// idValido confere o formato do id do YouTube: 11 caracteres de [A-Za-z0-9_-].
//
// A validação existe para o id poder virar caminho em disco SEM travessia: nem `.`, nem `/`,
// nem `..` passam. É uma lista de permissão de caracteres, não uma lista de proibições —
// proibir "os ruins" deixa sempre um de fora.
//
// O tamanho fixo 11 é o formato do YouTube desde sempre. Se algum dia mudar, o sintoma é
// claro (id recusado, erro na criação do pedido), e o conserto é aqui — muito melhor que
// aceitar qualquer coisa e descobrir tarde que viramos donos de um caminho arbitrário.
func idValido(id string) bool {
	if len(id) != 11 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
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
