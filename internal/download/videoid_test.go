package download

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVideoID(t *testing.T) {
	// Ids reais têm 11 caracteres. Os fixtures antigos ("abc123", "xyz") eram curtos, e a
	// validação de formato passou a recusá-los — de propósito: o id vira nome de diretório.
	const id = "mg83gcM4ctw" // 11 chars, id real de um culto
	casos := []struct {
		url  string
		quer string
	}{
		// formas reais de endereçar o vídeo
		{"https://www.youtube.com/watch?v=" + id, id},
		{"https://youtube.com/watch?v=" + id + "&t=90s", id},
		{"https://www.youtube.com/watch?v=" + id + "&list=PL123&index=2", id},
		{"https://m.youtube.com/watch?v=" + id, id},
		{"https://youtu.be/" + id, id},
		{"https://youtu.be/" + id + "?t=42", id},
		{"https://www.youtube.com/embed/" + id, id},
		{"https://www.youtube.com/shorts/" + id, id},
		{"https://music.youtube.com/watch?v=" + id, id},
		{"HTTPS://WWW.YOUTUBE.COM/watch?v=" + id, id}, // host em maiúsculas
		{"  https://youtu.be/" + id + "  ", id},       // espaços colados no formulário

		// TRANSMISSÃO AO VIVO — é como o YouTube endereça os cultos desta igreja.
		{"https://www.youtube.com/live/" + id, id},
		{"https://www.youtube.com/live/" + id + "?feature=share", id},
		{"https://youtube.com/live/" + id + "?si=abc&t=120", id},

		// não é YouTube, ou não dá id
		{"https://vimeo.com/12345", ""},
		{"https://www.youtube.com/", ""},
		{"https://www.youtube.com/channel/UC123", ""},
		{"não é url :://", ""},
		{"", ""},

		// formato inválido: o id vira CAMINHO, então tem de ser recusado antes disso
		{"https://youtu.be/abc123", ""},          // curto demais
		{"https://youtu.be/abcdefghijkl", ""},    // longo demais (12)
		{"https://www.youtube.com/watch?v=a.b", ""},
	}
	for _, c := range casos {
		if got := VideoID(c.url); got != c.quer {
			t.Errorf("VideoID(%q) = %q, quero %q", c.url, got, c.quer)
		}
	}
}

// TestVideoIDNaoViraCaminhoHostil é o teste que justifica a validação existir. O id passou a
// nomear diretório no cache (videos/<id>/); sem validar, a URL escolhe onde escrevemos.
//
// Não basta conferir que a saída é vazia: o teste também monta o caminho que sairia e prova
// que ele não escapa da raiz — é a consequência que importa, não a string.
func TestVideoIDNaoViraCaminhoHostil(t *testing.T) {
	hostis := []string{
		"https://youtu.be/../../etc/passwd",
		"https://youtu.be/..",
		"https://www.youtube.com/watch?v=../../..",
		"https://www.youtube.com/watch?v=/etc/passwd",
		"https://www.youtube.com/watch?v=a/b/c",
		"https://www.youtube.com/live/" + strings.Repeat("../", 5),
		"https://www.youtube.com/watch?v=" + strings.Repeat("A", 300),
	}
	const raiz = "/tmp/videos"
	for _, u := range hostis {
		id := VideoID(u)
		if id != "" {
			t.Errorf("VideoID(%q) devolveu %q: entrada hostil não pode virar id", u, id)
			continue
		}
		// E a contraprova do porquê: com id vazio, quem monta caminho não sai da raiz.
		if p := filepath.Join(raiz, id, "video.mp4"); !strings.HasPrefix(filepath.Clean(p), raiz) {
			t.Errorf("o caminho montado escapou da raiz: %s", p)
		}
	}
}
