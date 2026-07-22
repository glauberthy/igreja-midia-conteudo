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
	margemFim := flag.Float64("margem-fim", 0.4, "margem de recuo no fim do corte, em segundos (evita capturar a fala seguinte)")
	flag.Parse()

	if *id == "" {
		fmt.Fprintln(os.Stderr, "uso: render -id ID [-base trabalho] [-out finalizados] [-cand arquivo.json] [-bin ffmpeg] [-margem-fim 0.4]")
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

	r := &video.Renderizador{Exec: video.ExecutorReal{}, Bin: *bin, BaseDir: *base, OutDir: *out, MargemFimMs: margemFimMs}
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
