package servidor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// O CACHE POR VÍDEO (spec-05 v3) existe para um número: cada pedido rebaixava ~570 MB do mesmo
// culto. Estes testes cobrem o que esse ganho não pode custar — sobrescrever os candidatos de
// outro pedido do mesmo culto.

// transcricaoLongaDoCulto espalha fala ao longo de 40 MINUTOS. As fixtures existentes cobrem
// só os primeiros minutos, e com elas um pedido de janela 00:20:00–00:35:00 receberia recorte
// VAZIO — o teste falharia por falta de fala, não por defeito do cache. Dois pedidos de janelas
// diferentes precisam de um culto que tenha conteúdo nas duas.
func transcricaoLongaDoCulto() string {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		seg := i * 30 // 0 a 39min30
		fmt.Fprintf(&b, "[%02d:%02d:%02d] frase numero %d do culto termina aqui.\n",
			seg/3600, (seg%3600)/60, seg%60, i)
	}
	return b.String()
}

// servidorComCache é como servidorPesada, mas com id de pedido INCREMENTAL: dois pedidos no
// mesmo servidor é justamente o caso que o cache introduz.
func servidorComCache(t *testing.T, sel *selecionadorFake, bv *baixadorVideoFake, rf *renderFake) *Servidor {
	t.Helper()
	base, out := t.TempDir(), t.TempDir()
	rf.outDir = out
	n := 0
	return Novo(Opcoes{
		Baixador:       &baixadorFake{transc: transcricaoLongaDoCulto(), base: base},
		Selecionador:   sel,
		BaixadorVideo:  bv,
		Renderizador:   rf,
		BaseDir:        base,
		OutDir:         out,
		LogRodadasPath: filepath.Join(base, "rodadas.md"),
		TemposPath:     filepath.Join(base, "tempos.csv"),
		CortesPath:     filepath.Join(base, "cortes.csv"),
		Agora:          func() time.Time { return time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC) },
		GerarID:        func() string { n++; return fmt.Sprintf("pedido-%d", n) },
	})
}

// criarPedido cria um pedido com janela explícita e devolve o id.
func criarPedido(t *testing.T, s *Servidor, url, inicio, fim string) string {
	t.Helper()
	body := fmt.Sprintf(`{"youtube_url":%q,"inicio":%q,"fim":%q}`, url, inicio, fim)
	req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("criação falhou: código %d, corpo %q", rec.Code, rec.Body.String())
	}
	var resp struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta da criação não é JSON: %v", err)
	}
	return resp.ID
}

// TestDoisPedidosDoMesmoCultoUmDownloadECandidatosSeparados é o teste central da Parte 2.
//
// Dois pedidos do MESMO vídeo com JANELAS DIFERENTES — o caso real: uma transmissão com duas
// pregações, ou o operador refazendo com outra janela. O que se exige:
//
//	1. o vídeo é baixado UMA vez (é o ganho: ~35 s e ~570 MB);
//	2. cada pedido tem o SEU candidatos.corrigido.json — nenhum sobrescreve o outro (é por isso
//	   que o id do vídeo NÃO nomeia a pasta do pedido);
//	3. cada pedido tem a SUA transcrição recortada, com a proveniência declarada.
func TestDoisPedidosDoMesmoCultoUmDownloadECandidatosSeparados(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	sel := &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "trecho", Start: "00:00:30.000", End: "00:01:04.000", DurationSeconds: 34, Score: 80},
	}}
	s := servidorComCache(t, sel, bv, rf)
	const url = "https://www.youtube.com/live/cultoTeste1" // transmissão ao vivo: o caso da igreja

	// Pedido 1: primeira pregação.
	id1 := criarPedido(t, s, url, "00:00:00", "00:10:00")
	esperarStatus(t, s, id1, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id1, []int{0})
	esperarStatus(t, s, id1, pipeline.EstadoConcluido)

	bv.mu.Lock()
	baixouNoPrimeiro := bv.chamado
	bv.chamado = false // zera para medir o segundo
	bv.mu.Unlock()
	if !baixouNoPrimeiro {
		t.Fatal("o primeiro pedido tinha de baixar o vídeo")
	}

	// Pedido 2: MESMO vídeo, outra janela.
	id2 := criarPedido(t, s, url, "00:20:00", "00:35:00")
	if id2 == id1 {
		t.Fatal("os dois pedidos receberam o mesmo id: o teste não provaria nada")
	}
	esperarStatus(t, s, id2, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id2, []int{0})
	esperarStatus(t, s, id2, pipeline.EstadoConcluido)

	// (1) NÃO baixou de novo.
	bv.mu.Lock()
	baixouNoSegundo := bv.chamado
	bv.mu.Unlock()
	if baixouNoSegundo {
		t.Error("o segundo pedido baixou o vídeo de novo: o cache não pegou, e são ~570 MB e " +
			"~35 s jogados fora por pedido")
	}

	// (2) Candidatos separados, um por pedido.
	for _, id := range []string{id1, id2} {
		if _, err := os.Stat(filepath.Join(s.baseDir, id, "candidatos.corrigido.json")); err != nil {
			t.Errorf("pedido %s sem candidatos próprios: %v", id, err)
		}
	}

	// (3) Transcrição recortada por pedido, com proveniência declarada e janelas DIFERENTES.
	ped1, err := pipeline.Carregar(s.baseDir, id1)
	if err != nil {
		t.Fatal(err)
	}
	ped2, err := pipeline.Carregar(s.baseDir, id2)
	if err != nil {
		t.Fatal(err)
	}
	if ped1.Recorte == nil || ped2.Recorte == nil {
		t.Fatalf("falta a proveniência do recorte: %+v / %+v", ped1.Recorte, ped2.Recorte)
	}
	if ped1.Recorte.VideoID != "cultoTeste1" || ped2.Recorte.VideoID != "cultoTeste1" {
		t.Errorf("os dois recortes deviam vir do mesmo vídeo: %+v / %+v", ped1.Recorte, ped2.Recorte)
	}
	if ped1.Recorte.Inicio == ped2.Recorte.Inicio {
		t.Errorf("as janelas declaradas são iguais (%s): o teste exige janelas diferentes",
			ped1.Recorte.Inicio)
	}
	t1 := lerArquivo(t, filepath.Join(s.baseDir, id1, "transcricao.txt"))
	t2 := lerArquivo(t, filepath.Join(s.baseDir, id2, "transcricao.txt"))
	if t1 == t2 {
		t.Error("as duas transcrições recortadas são idênticas: o recorte não respeitou a janela " +
			"de cada pedido (e a seleção do segundo veria o texto do primeiro)")
	}
	if t1 == "" || t2 == "" {
		t.Error("transcrição recortada vazia: a seleção não teria o que ler")
	}

	// E o cache tem UM diretório para o culto, não um por pedido.
	dirCache, _ := s.cache.DirVideo("cultoTeste1")
	if _, err := os.Stat(filepath.Join(dirCache, "video.mp4")); err != nil {
		t.Errorf("o vídeo não está no cache do culto: %v", err)
	}
}

// TestSegundoPedidoNaoRebaixaALegenda: a legenda também é do culto. São só 3 s, mas o ponto é o
// mesmo — e sem isto o cache do vídeo conviveria com um download de legenda por pedido.
func TestSegundoPedidoNaoRebaixaALegenda(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)
	bf := s.baixador.(*baixadorFake)
	const url = "https://youtu.be/cultoTeste1"

	id1 := criarPedido(t, s, url, "00:00:00", "00:10:00")
	esperarStatus(t, s, id1, pipeline.EstadoAguardandoAprovacao)
	bf.mu.Lock()
	chamadasDepoisDoPrimeiro := bf.chamadas
	bf.mu.Unlock()

	id2 := criarPedido(t, s, url, "00:20:00", "00:35:00")
	esperarStatus(t, s, id2, pipeline.EstadoAguardandoAprovacao)
	bf.mu.Lock()
	chamadasDepoisDoSegundo := bf.chamadas
	bf.mu.Unlock()

	if chamadasDepoisDoSegundo != chamadasDepoisDoPrimeiro {
		t.Errorf("a legenda foi baixada de novo (%d → %d chamadas): ela é do culto e já estava "+
			"no cache", chamadasDepoisDoPrimeiro, chamadasDepoisDoSegundo)
	}
	// E o segundo pedido tem transcrição, mesmo sem download: veio da derivação.
	if txt := lerArquivo(t, filepath.Join(s.baseDir, id2, "transcricao.txt")); txt == "" {
		t.Error("o segundo pedido ficou sem transcrição: o acerto de cache tem de derivar do " +
			"que já está em disco, não pular a derivação")
	}
}

// TestRetomarEncontraOVideoNoCacheEPulaODownload: o -retomar é o caminho de iteração do
// desenvolvimento e do operador que quer regerar. Com o cache, ele não pode voltar a baixar.
func TestRetomarEncontraOVideoNoCacheEPulaODownload(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)
	const url = "https://youtu.be/cultoTeste1"

	// Ciclo completo, para o culto entrar no cache e o pedido ficar em disco.
	id := criarPedido(t, s, url, "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id, []int{0})
	esperarStatus(t, s, id, pipeline.EstadoConcluido)

	// Servidor NOVO (como reiniciar o processo), apontando para as mesmas pastas.
	bv2 := &baixadorVideoFake{}
	rf2 := &renderFake{outDir: s.outDir}
	s2 := Novo(Opcoes{
		Baixador:       &baixadorFake{transc: transcricaoLongaDoCulto(), base: s.baseDir},
		Selecionador:   candsJanela(),
		BaixadorVideo:  bv2,
		Renderizador:   rf2,
		BaseDir:        s.baseDir,
		VideosDir:      s.cache.Dir,
		OutDir:         s.outDir,
		LogRodadasPath: filepath.Join(s.baseDir, "rodadas.md"),
		TemposPath:     filepath.Join(s.baseDir, "tempos.csv"),
		CortesPath:     filepath.Join(s.baseDir, "cortes.csv"),
		Agora:          func() time.Time { return time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "outro" },
	})
	if err := s2.Retomar(id); err != nil {
		t.Fatalf("Retomar: %v", err)
	}
	aprovarJSON(t, s2, id, []int{0})
	esperarStatus(t, s2, id, pipeline.EstadoConcluido)

	bv2.mu.Lock()
	baixou := bv2.chamado
	bv2.mu.Unlock()
	if baixou {
		t.Error("a retomada baixou o vídeo de novo: ele está no cache do culto, e é justamente " +
			"o caso que o -retomar existe para evitar")
	}
	// E o render recebeu o arquivo DO CACHE, com a origem do video.json.
	rf2.mu.Lock()
	usado, origem := rf2.videoPath, rf2.origemMs
	rf2.mu.Unlock()
	dirCache, _ := s2.cache.DirVideo("cultoTeste1")
	if usado != filepath.Join(dirCache, "video.mp4") {
		t.Errorf("o render cortou %q; esperado o vídeo do cache em %s", usado, dirCache)
	}
	if origem != 0 {
		t.Errorf("origem = %d, quero 0 (o vídeo do cache é o inteiro)", origem)
	}
}

// TestURLSemIDDeVideoERecusadaNaCriacao: sem id não há chave de cache, e cada pedido voltaria a
// rebaixar. Recusar na entrada, com a lista das formas aceitas, é melhor que falhar no meio da
// fase leve.
func TestURLSemIDDeVideoERecusadaNaCriacao(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{transc: "x"}, &selecionadorFake{})
	for _, url := range []string{
		"https://www.youtube.com/",
		"https://www.youtube.com/channel/UC123",
		"https://youtu.be/curto",
	} {
		body := fmt.Sprintf(`{"youtube_url":%q,"inicio":"00:00:00","fim":"00:10:00"}`, url)
		req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: código %d, esperado 400", url, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "youtube.com/live/") {
			t.Errorf("%s: a mensagem devia citar as formas aceitas (inclusive /live/): %s",
				url, rec.Body.String())
		}
	}
}

func lerArquivo(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
