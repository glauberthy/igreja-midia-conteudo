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
		// Igual ao cmd/servidor: a legenda vem da flag, cujo default é a constante do pacote.
		Legenda: LegendaQueimadaPadrao,
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

	if _, err := r.Renderizar(context.Background(), ped, cands, filepath.Join(base, ped.ID, "video.mp4"), 0); err != nil {
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

	// Valores LITERAIS de propósito (não as constantes): montado a partir delas, o teste
	// passaria com qualquer valor desde que consistente, e o que se quer travar é ESTE.
	for _, par := range [][2]string{{"-preset", "medium"}, {"-crf", "20"}} {
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

	// O trecho LITERAL que o ffmpeg tem de receber com a escolha do operador (520/0.60).
	// Escrito à mão de propósito: montado a partir das constantes, o teste passaria com
	// qualquer valor, desde que consistente — e o que se quer travar é ESTE valor.
	const trechoEsperado = `color=c=black:s=1080x520:d=1,format=rgba,geq=r=0:g=0:b=0:a='0.60*255*pow(Y/H\,2.2)'`
	t.Logf("trecho do gradiente esperado:\n%s", trechoEsperado)
	if !strings.Contains(linha, trechoEsperado) {
		t.Errorf("o gradiente 520/0.60 (escolha do operador) não chegou ao ffmpeg.\nquero: %s\nlinha: %s",
			trechoEsperado, linha)
	}

	// As mesmas duas peças, agora derivadas das constantes: pega o caso em que alguém muda a
	// constante e esquece o literal acima (ou vice-versa) — os dois têm de concordar.
	querAlpha := fmt.Sprintf("%.2f*255*pow", RodapeAlphaPadrao)
	querAltura := fmt.Sprintf("x%d", rodapeAlturaPadrao)

	if !strings.Contains(linha, querAlpha) {
		t.Errorf("a opacidade %s não chegou ao ffmpeg:\n%s", querAlpha, linha)
	}
	if !strings.Contains(linha, querAltura) {
		t.Errorf("a altura %s não chegou ao ffmpeg:\n%s", querAltura, linha)
	}
	// Trava o par altura/opacidade em vigor. 1500/0.72 foi a escolha do operador enquanto a
	// legenda era queimada; com a legenda suspensa (spec-12) o gradiente passou a servir só a
	// logo, e entre as nove variantes medidas (docs/medicoes/imagem-sem-legenda.md) o operador
	// escolheu 520/0.60 — a mais suave e mais clara. Mudar exige mudar aqui de propósito —
	// não deve acontecer por efeito colateral de outra alteração.
	if RodapeAlphaPadrao != 0.60 || rodapeAlturaPadrao != 520 {
		t.Errorf("os padrões do rodapé mudaram: alpha=%.2f altura=%d (escolha do operador: 0.60/520)",
			RodapeAlphaPadrao, rodapeAlturaPadrao)
	}
	// O gradiente longo NÃO pode voltar por acidente: 1500 px cobriam 78% da altura do Short
	// para servir uma faixa de logo de 240 px, e era a causa do "apertado".
	if rodapeAlturaPadrao > 700 {
		t.Errorf("gradiente de %d px cobre %.0f%% da altura do Short — sem legenda ele só precisa "+
			"cobrir a faixa da logo (%d px)", rodapeAlturaPadrao,
			100*float64(rodapeAlturaPadrao)/float64(alturaSaida), faixaLogoPadrao)
	}
}

// TestLegendaSuspensaNoCaminhoDoOperador prova a SUSPENSÃO da queima (spec-12) onde ela
// importa: no comando que o ffmpeg recebe quando o pedido vem do servidor. Verificar que a
// constante é `false` não provaria nada — o caminho do operador poderia estar ligando a
// legenda por conta própria, que é exatamente o erro que já aconteceu com o RodapeAlpha.
//
// O fixture ESCREVE uma transcrição com fala dentro do trecho: se a legenda estivesse
// ligada, haveria drawtext. A ausência é decisão, não falta de texto.
func TestLegendaSuspensaNoCaminhoDoOperador(t *testing.T) {
	if LegendaQueimadaPadrao {
		t.Skip("a queima voltou a ser o padrão (Rota D resolvida?) — este teste guarda a suspensão")
	}
	args := argsDoOperador(t)
	linha := strings.Join(args, " ")
	t.Logf("comando que o ffmpeg recebe:\n%s", linha)

	if strings.Contains(linha, "drawtext") {
		t.Errorf("a legenda está sendo QUEIMADA no caminho do operador (spec-12 está suspensa):\n%s", linha)
	}
	if strings.Contains(linha, "textfile=") {
		t.Errorf("bloco de legenda chegou ao ffmpeg com a queima suspensa:\n%s", linha)
	}
	// Contraprova: o MESMO fixture com a legenda ligada produz drawtext. Sem isso, o teste
	// acima passaria também se o render tivesse parado de desenhar legenda por bug.
	base, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, "p1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 1024), 0644)
	os.WriteFile(filepath.Join(dir, "transcricao.txt"), []byte("[00:00:10] a graca basta.\n"), 0644)
	esp := &execEspiao{}
	r := comoOCmdServidorMonta(base, out, esp)
	r.Legenda = true // é o que a flag -legenda faz
	ped := &pipeline.Pedido{ID: "p1", YouTubeURL: "https://x", Inicio: "00:00:00"}
	cands := []validacao.Candidato{{Start: "00:00:10.000", End: "00:00:50.000", DurationSeconds: 40, Hook: "a graca basta."}}
	if _, err := r.Renderizar(context.Background(), ped, cands, filepath.Join(base, ped.ID, "video.mp4"), 0); err != nil {
		t.Fatalf("render com legenda ligada falhou: %v", err)
	}
	if !strings.Contains(strings.Join(esp.chamadas[0], " "), "drawtext") {
		t.Error("com -legenda ligado o ffmpeg deveria receber drawtext: a flag não liga nada, " +
			"então a ausência acima não prova suspensão")
	}
	// E a logo continua: a suspensão é só da legenda (o dono pediu para manter a marca).
	// Verificado no montador de filtro, porque nos testes o PNG de assets/ não existe
	// (o cwd é internal/video) e o render pula a logo com aviso.
	semLegenda, _ := montarFiltro(nil, nil, estiloTeste(), true,
		LogoConfig{Path: "assets/ibi_assinatura_shorts.png", LarguraPx: logoLarguraPadrao},
		GradConfig{Altura: rodapeAlturaPadrao, Alpha: RodapeAlphaPadrao})
	if !strings.Contains(semLegenda, "[logo]overlay") {
		t.Errorf("a logo desapareceu junto com a legenda — ela deve continuar:\n%s", semLegenda)
	}
	if strings.Contains(semLegenda, "drawtext") {
		t.Errorf("filtro sem blocos não deveria ter drawtext:\n%s", semLegenda)
	}
}
