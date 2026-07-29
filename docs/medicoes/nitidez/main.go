// Comando nitidez: mede a NITIDEZ de um PNG pela variância do laplaciano, mais o brilho
// médio (útil para medir o quanto o gradiente do rodapé escurece a imagem).
//
// Existe porque a máquina do projeto não tem OpenCV/numpy: a medição do dono
// (`cv2.Laplacian(...).var()`) não roda aqui. O cálculo é o mesmo, em Go puro:
//
//	cinza = 0.299R + 0.587G + 0.114B      (mesma luma do cv2.COLOR_BGR2GRAY)
//	laplaciano = kernel 3x3 [[0,1,0],[1,-4,1],[0,1,0]]   (mesmo do cv2.Laplacian)
//	nitidez = variância dos valores do laplaciano (bordas excluídas)
//
// IMPORTANTE — comparar só é honesto se as duas imagens tiverem o MESMO tamanho: a
// variância do laplaciano cresce com a resolução. O recorte e a normalização de tamanho
// ficam FORA daqui, de propósito, num comando de ffmpeg visível na linha de comando
// (ver docs/medicoes/nitidez.md). Este programa só mede o PNG que recebe.
//
// Uso:
//
//	go run ./docs/medicoes/nitidez recorte_a.png recorte_b.png
package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: nitidez <imagem.png> [outra.png ...]")
		os.Exit(2)
	}
	fmt.Printf("%-52s %9s %9s %9s %9s\n", "imagem", "tamanho", "laplac.", "luma", "desvio")
	for _, p := range os.Args[1:] {
		m, err := abrir(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro em %s: %v\n", p, err)
			os.Exit(1)
		}
		cinza, w, h := cinzaDe(m)
		lap := varianciaLaplaciano(cinza, w, h)
		luma, desvio := mediaDesvio(cinza)
		fmt.Printf("%-52s %4dx%-4d %9.2f %9.2f %9.2f\n", nomeCurto(p), w, h, lap, luma, desvio)
	}
}

func abrir(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, _, err := image.Decode(f)
	return m, err
}

// cinzaDe converte para luma (mesma fórmula do cv2.COLOR_BGR2GRAY) em float, 0..255.
func cinzaDe(m image.Image) ([]float64, int, int) {
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := m.At(b.Min.X+x, b.Min.Y+y).RGBA() // 0..65535
			out[y*w+x] = (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257.0
		}
	}
	return out, w, h
}

// varianciaLaplaciano aplica o kernel [[0,1,0],[1,-4,1],[0,1,0]] e devolve a variância da
// resposta. Ignora a moldura de 1 px (onde o kernel não cabe), como o cv2 com BORDER_DEFAULT
// não faz — a diferença é desprezível em recortes de centenas de px e evita inventar borda.
func varianciaLaplaciano(cinza []float64, w, h int) float64 {
	if w < 3 || h < 3 {
		return 0
	}
	var soma, somaQ float64
	n := 0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			c := cinza[y*w+x]
			v := cinza[(y-1)*w+x] + cinza[(y+1)*w+x] + cinza[y*w+x-1] + cinza[y*w+x+1] - 4*c
			soma += v
			somaQ += v * v
			n++
		}
	}
	media := soma / float64(n)
	return somaQ/float64(n) - media*media
}

func mediaDesvio(cinza []float64) (float64, float64) {
	var soma, somaQ float64
	for _, v := range cinza {
		soma += v
		somaQ += v * v
	}
	n := float64(len(cinza))
	media := soma / n
	return media, math.Sqrt(somaQ/n - media*media)
}

func nomeCurto(p string) string {
	if len(p) <= 52 {
		return p
	}
	return "..." + p[len(p)-49:]
}
