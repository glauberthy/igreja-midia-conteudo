// Comando render: gera os Shorts de um pedido já baixado (spec-03) e selecionado
// (spec-07). Lê o pedido em trabalho/<id>/pedido.json (apenas metadados: id, url,
// início, fim, status) e os candidatos VALIDADOS do arquivo de seleção, e produz
// finalizados/<id>/short_NN.mp4.
//
// Fonte única de verdade dos candidatos (spec-09): o arquivo de seleção validado
// (candidatos.corrigido.json), NUNCA uma cópia embutida no pedido. A flag -cand
// explícita sempre vence; sem candidato validado, erro claro (não renderiza material
// não-validado — regra inviolável nº 3).
//
// Requer o ffmpeg instalado (ver README). Uso:
//
//	go run . -id teste
//	go run . -id teste -cand trabalho/teste/candidatos.corrigido.json
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
	"srtclean/internal/video"
)

func main() {
	id := flag.String("id", "", "identificador do pedido (obrigatório)")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	out := flag.String("out", "finalizados", "pasta raiz dos Shorts finais")
	cand := flag.String("cand", "", "arquivo de candidatos corrigidos (padrão: <base>/<id>/candidatos.corrigido.json)")
	bin := flag.String("bin", "ffmpeg", "binário do ffmpeg")
	margemFim := flag.Float64("margem-fim", 0.0, "margem de recuo no fim do corte, em segundos (0 = corta no end cheio; ver spec-10)")
	fonte := flag.String("fonte", "", "caminho do .ttf da legenda (spec-12; vazio = Google Sans Flex Bold em assets/)")
	fonteTam := flag.Int("fonte-tam", 0, "tamanho da fonte da legenda em px (0 = default)")
	legendaCPL := flag.Int("legenda-cpl", 0, "caracteres por linha da legenda; governa o ritmo de troca (0 = default)")
	logo := flag.String("logo", "", "caminho do PNG da logo do rodapé (spec-13; vazio = assets/logo_ibi_gsf.png)")
	logoLarg := flag.Int("logo-larg", 0, "largura da logo no vídeo em px (0 = default)")
	logoBaixo := flag.Int("logo-ajuste-y", 0, "ajuste vertical da logo a partir do centro da faixa (px; + desce, - sobe)")
	rodapeAlpha := flag.Float64("rodape-escuro", 1.00, "opacidade do gradiente escuro no rodapé (0 = sem gradiente; ajuda a logo/legenda em fundo claro)")
	rodapeAltura := flag.Int("rodape-altura", 0, "altura do gradiente escuro do rodapé em px (0 = default)")
	flag.Parse()

	if *id == "" {
		fmt.Fprintln(os.Stderr, "uso: render -id ID [-base trabalho] [-out finalizados] [-cand arquivo.json] [-bin ffmpeg] [-margem-fim 0] [-fonte ...ttf] [-fonte-tam N] [-legenda-cpl N]")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if *margemFim < 0 {
		fmt.Fprintln(os.Stderr, "erro: -margem-fim não pode ser negativo")
		os.Exit(2)
	}

	ped, err := pipeline.Carregar(*base, *id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: não carreguei o pedido %q: %v\n", *id, err)
		os.Exit(1)
	}

	// Fonte ÚNICA de verdade dos candidatos (spec-09): o arquivo de seleção validado.
	// Precedência: -cand explícito sempre vence; senão, o padrão da pasta do pedido.
	// Sem candidato validado: erro claro, nunca fallback para material não-validado.
	candPath := caminhoCandidatos(*base, *id, *cand)
	cands, err := carregarCandidatos(candPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: nenhum candidato validado encontrado em %s; rode a seleção antes (%v)\n", candPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "render: lendo %s, %d candidato(s)\n", candPath, len(cands))

	margemFimMs := int(math.Round(*margemFim * 1000))
	fmt.Fprintf(os.Stderr, "render: margem de recuo no fim = %.3fs (corte termina em end - margem)\n", *margemFim)

	r := &video.Renderizador{
		Exec: video.ExecutorReal{}, Bin: *bin, BaseDir: *base, OutDir: *out,
		MargemFimMs:   margemFimMs,
		FontePath:     *fonte,
		TamanhoFonte:  *fonteTam,
		CharsPorLinha: *legendaCPL,
		LogoPath:      *logo,
		LogoLargura:   *logoLarg,
		LogoAjusteY:   *logoBaixo,
		RodapeAlpha:   *rodapeAlpha,
		RodapeAltura:  *rodapeAltura,
	}
	paths, err := r.Renderizar(context.Background(), ped, cands)

	// Persiste apenas o ESTADO do pedido (o pedido não carrega candidatos — spec-09).
	if ped.Status != pipeline.EstadoErro {
		ped.Status = pipeline.EstadoConcluido
	}
	if salvarErr := ped.Salvar(*base); salvarErr != nil {
		fmt.Fprintf(os.Stderr, "aviso: não salvei o pedido: %v\n", salvarErr)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "erro na renderização: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "ok: %d Short(s) gerado(s) em %s/%s/\n", len(paths), *out, *id)
	for _, p := range paths {
		fmt.Println(p)
	}
}

// caminhoCandidatos resolve de onde ler os candidatos validados (spec-09): a flag
// -cand explícita SEMPRE vence; se vazia, o padrão <base>/<id>/candidatos.corrigido.json.
func caminhoCandidatos(base, id, candFlag string) string {
	if candFlag != "" {
		return candFlag
	}
	return filepath.Join(base, id, "candidatos.corrigido.json")
}

// carregarCandidatos lê um arquivo {"candidatos": [...]} e devolve a lista. Arquivo
// ausente ou sem candidatos é erro (nunca fallback para material não-validado).
func carregarCandidatos(path string) ([]validacao.Candidato, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Candidatos []validacao.Candidato `json:"candidatos"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if len(doc.Candidatos) == 0 {
		return nil, errors.New("arquivo sem candidatos")
	}
	return doc.Candidatos, nil
}
