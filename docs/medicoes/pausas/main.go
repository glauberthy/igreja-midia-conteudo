// Comando pausas: gera (ou inspeciona) as PAUSAS de fala de um culto do cache, pelo mesmo caminho
// que o servidor usa — video.DetectarPausas + videocache.GravarPausas.
//
// Existe na mesma estante do docs/medicoes/nitidez: é ferramenta de medição, para o dono afinar o
// limiar num culto real e ver o efeito sem subir o servidor nem refazer um pedido. O servidor gera
// as pausas sozinho; isto serve para regerar com outro parâmetro e para conferir o resultado.
//
// Uso:
//
//	go run ./docs/medicoes/pausas -id fZGyLBofmmo                    # gera com os padrões medidos
//	go run ./docs/medicoes/pausas -id fZGyLBofmmo -db -35 -min 500   # outro limiar
//	go run ./docs/medicoes/pausas -id fZGyLBofmmo -ler -de 5400000 -ate 5412000   # só inspeciona
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"srtclean/internal/video"
	"srtclean/internal/videocache"
)

func main() {
	id := flag.String("id", "", "id do vídeo no cache (obrigatório)")
	dirVideos := flag.String("videos", videocache.DirPadrao, "raiz do cache por vídeo")
	db := flag.Int("db", video.PausaNoiseDBPadrao, "limiar de silêncio em dB")
	min := flag.Int("min", video.PausaMinMsPadrao, "duração mínima da pausa em ms")
	ler := flag.Bool("ler", false, "não gera; só lê o pausas.json que já está em disco")
	de := flag.Int("de", 0, "início da janela a listar (ms)")
	ate := flag.Int("ate", 0, "fim da janela a listar (ms; 0 = não lista)")
	flag.Parse()
	if *id == "" {
		fmt.Fprintln(os.Stderr, "uso: -id <idDoVídeo>")
		os.Exit(2)
	}

	c := videocache.Novo(*dirVideos)
	dir, err := c.DirVideo(*id)
	if err != nil {
		morrer(err)
	}

	if !*ler {
		o := video.OpcoesPausas{NoiseDB: *db, MinMs: *min}.ComPadroes()
		t0 := time.Now()
		ps, err := video.DetectarPausas(context.Background(), video.ExecutorReal{}, "ffmpeg",
			filepath.Join(dir, videocache.NomeVideo), o)
		if err != nil {
			morrer(err)
		}
		guardar := make([]videocache.Pausa, 0, len(ps))
		for _, p := range ps {
			guardar = append(guardar, videocache.Pausa{InicioMs: p.InicioMs, FimMs: p.FimMs})
		}
		if err := c.GravarPausas(*id, videocache.AnalisePausas{
			NoiseDB: o.NoiseDB, MinMs: o.MinMs, Pausas: guardar,
		}); err != nil {
			morrer(err)
		}
		fmt.Printf("%d pausas em %.1fs (limiar %d dB, mínimo %d ms) -> %s\n",
			len(guardar), time.Since(t0).Seconds(), o.NoiseDB, o.MinMs,
			filepath.Join(dir, videocache.NomePausas))
	}

	a, err := c.LerPausas(*id)
	if err != nil {
		morrer(err)
	}
	fmt.Printf("em disco: %d pausas, receita %d dB / %d ms, gerado em %s\n",
		len(a.Pausas), a.NoiseDB, a.MinMs, a.GeradoEm.Format("2006-01-02 15:04"))
	if *ate > 0 {
		fmt.Printf("\njanela %d–%d ms:\n", *de, *ate)
		for _, p := range a.Pausas {
			if p.InicioMs >= *de && p.InicioMs <= *ate {
				fmt.Printf("  %8d -> %8d  (%4d ms)  %s\n", p.InicioMs, p.FimMs, p.DuracaoMs(),
					hms(p.InicioMs))
			}
		}
	}
}

func hms(ms int) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, (ms/60000)%60, (ms/1000)%60, ms%1000)
}

func morrer(err error) {
	fmt.Fprintln(os.Stderr, "erro:", err)
	os.Exit(1)
}
