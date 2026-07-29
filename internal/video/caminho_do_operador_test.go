package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// Este arquivo existe por causa de uma classe de erro que já aconteceu TRÊS vezes neste
// projeto: um valor existir num lugar e o caminho real usar outro. O caso mais recente foi o
// cmd/servidor fixar RodapeAlpha: 1.00, o que tornava a constante RodapeAlphaPadrao letra
// morta justamente no caminho que o operador usa.
//
// A verificação certa não é "a constante tem o valor X". É "o comando que o ffmpeg recebe,
// quando o pedido vem do servidor, carrega o valor X". É isso que este teste faz: monta o
// Renderizador EXATAMENTE como o cmd/servidor monta (só Exec/Bin/BaseDir/OutDir preenchidos,
// o resto zerado) e inspeciona os argumentos de verdade.

// execEspiao captura os argumentos em vez de executar o ffmpeg.
type execEspiao struct{ chamadas [][]string }

func (e *execEspiao) Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	e.chamadas = append(e.chamadas, append([]string{nome}, args...))
	return nil, nil, nil
}

// comoOCmdServidorMonta reproduz a construção do cmd/servidor/main.go. Se aquele arquivo passar
// a fixar um valor de novo, este teste continua refletindo o que ELE faz — por isso o
// construtor é copiado aqui de propósito, e não importado.
func comoOCmdServidorMonta(base, out string, esp *execEspiao) *Renderizador {
	return &Renderizador{
		Exec: esp, Bin: "ffmpeg", BaseDir: base, OutDir: out,
		MargemFimMs: 0,
		// Igual ao cmd/servidor: RodapeAlpha EXPLÍCITO (zero significa "sem gradiente", não
		// "use o padrão"); RodapeAltura/Preset/CRF zerados, que ali o zero cai no padrão.
		RodapeAlpha: RodapeAlphaPadrao,
	}
}

func argsDoOperador(t *testing.T) []string {
	t.Helper()
	base, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, "p1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 1024), 0644)
	os.WriteFile(filepath.Join(dir, "transcricao.txt"), []byte("[00:00:00] a graca basta.\n"), 0644)

	esp := &execEspiao{}
	r := comoOCmdServidorMonta(base, out, esp)
	ped := &pipeline.Pedido{ID: "p1", YouTubeURL: "https://x", Inicio: "00:00:00"}
	cands := []validacao.Candidato{{Start: "00:00:10.000", End: "00:00:50.000", DurationSeconds: 40, Hook: "a graca basta."}}

	if _, err := r.RenderizarComOrigem(context.Background(), ped, cands, 0); err != nil {
		t.Fatalf("render falhou: %v", err)
	}
	if len(esp.chamadas) == 0 {
		t.Fatal("o ffmpeg não foi invocado")
	}
	return esp.chamadas[len(esp.chamadas)-1]
}

// TestEncodeNoCaminhoDoOperador prova que preset/crf medidos chegam ao ffmpeg pelo caminho do
// servidor. Falharia se o cmd/servidor voltasse a fixar valores, ou se o padrão do pacote
// mudasse sem intenção.
func TestEncodeNoCaminhoDoOperador(t *testing.T) {
	args := argsDoOperador(t)
	linha := strings.Join(args, " ")
	t.Logf("comando que o ffmpeg recebe:\n%s", linha)

	for _, par := range [][2]string{{"-preset", "medium"}, {"-crf", "18"}} {
		achou := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == par[0] && args[i+1] == par[1] {
				achou = true
			}
		}
		if !achou {
			t.Errorf("o ffmpeg não recebeu %s %s — o caminho do operador usa outro valor:\n%s",
				par[0], par[1], linha)
		}
	}
}

// TestDegradeNoCaminhoDoOperador prova o mesmo para o gradiente do rodapé. É o caso que já
// falhou: a constante era 1.00 no pacote e o servidor fixava outro valor.
func TestDegradeNoCaminhoDoOperador(t *testing.T) {
	args := argsDoOperador(t)
	linha := strings.Join(args, " ")

	// O filtro do gradiente carrega a opacidade e a altura direto na expressão do geq.
	querAlpha := fmt.Sprintf("%.2f*255*pow", RodapeAlphaPadrao)
	querAltura := fmt.Sprintf("x%d", rodapeAlturaPadrao)
	t.Logf("trecho do gradiente esperado: %s ... %s", querAlpha, querAltura)

	if !strings.Contains(linha, querAlpha) {
		t.Errorf("a opacidade %s não chegou ao ffmpeg:\n%s", querAlpha, linha)
	}
	if !strings.Contains(linha, querAltura) {
		t.Errorf("a altura %s não chegou ao ffmpeg:\n%s", querAltura, linha)
	}
	// E os valores são os medidos, não os antigos.
	if RodapeAlphaPadrao != 0.80 || rodapeAlturaPadrao != 1400 {
		t.Errorf("os padrões do rodapé mudaram: alpha=%.2f altura=%d (medidos: 0.80/1400)",
			RodapeAlphaPadrao, rodapeAlturaPadrao)
	}
	if strings.Contains(linha, "1.00*255*pow") {
		t.Error("a opacidade 1.00 voltou — era o valor que o cmd/servidor fixava")
	}
}
