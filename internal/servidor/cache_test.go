package servidor

import (
	"encoding/csv"
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
		AcoesPath:      filepath.Join(base, "acoes.csv"),
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
//  1. o vídeo é baixado UMA vez (é o ganho: ~35 s e ~570 MB);
//  2. cada pedido tem o SEU candidatos.corrigido.json — nenhum sobrescreve o outro (é por isso
//     que o id do vídeo NÃO nomeia a pasta do pedido);
//  3. cada pedido tem a SUA transcrição recortada, com a proveniência declarada.
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

// TestCacheComVideoSemLegendaBaixaSoALegenda é o estado que a MIGRAÇÃO produz de verdade, e o
// que ele não pode custar.
//
// Depois de migrar, o cache tem video.mp4 + video.json e NÃO tem legenda — porque a legenda já
// tinha sido apagada da pasta do pedido pela limpeza (spec-06 lista legenda.srt como
// "baixa de novo"), então não havia o que copiar. Se "cache incompleto" disparasse o conjunto,
// os 820 MB que a migração acabou de salvar seriam rebaixados.
//
// Não há uma pergunta "está completo": há duas independentes, uma por artefato. Este teste é o
// que garante que elas continuem independentes.
func TestCacheComVideoSemLegendaBaixaSoALegenda(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)
	bf := s.baixador.(*baixadorFake)

	// Estado pós-migração: vídeo e índice no cache, legenda ausente.
	dirVideo, err := s.cache.DirVideo("cultoTeste1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirVideo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := escreverVideoFalso(filepath.Join(dirVideo, "video.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := s.cache.Registrar("cultoTeste1", 0, "Culto"); err != nil {
		t.Fatal(err)
	}
	mtimeAntes := modificadoEm(t, filepath.Join(dirVideo, "video.mp4"))

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	// BAIXOU a legenda (era o que faltava)...
	bf.mu.Lock()
	chamadasLegenda := bf.chamadas
	bf.mu.Unlock()
	if chamadasLegenda != 1 {
		t.Errorf("a legenda foi baixada %d vez(es), quero 1: ela faltava no cache", chamadasLegenda)
	}
	if !s.cache.TemLegenda("cultoTeste1") {
		t.Error("a legenda não entrou no cache")
	}

	// ...e a transcrição íntegra foi gerada dela (é derivada, não baixada).
	if _, err := os.Stat(filepath.Join(dirVideo, "transcricao.txt")); err != nil {
		t.Errorf("a transcrição íntegra do culto não foi gerada: %v", err)
	}

	// E O VÍDEO NÃO FOI TOCADO. É o ponto do teste.
	aprovarJSON(t, s, id, []int{0})
	esperarStatus(t, s, id, pipeline.EstadoConcluido)
	bv.mu.Lock()
	baixouVideo := bv.chamado
	bv.mu.Unlock()
	if baixouVideo {
		t.Error("o vídeo foi baixado de novo: faltava só a legenda, e rebaixar joga fora os " +
			"820 MB que a migração acabou de preservar")
	}
	if depois := modificadoEm(t, filepath.Join(dirVideo, "video.mp4")); depois != mtimeAntes {
		t.Errorf("o arquivo de vídeo foi reescrito (mtime %v → %v): nada devia encostar nele",
			mtimeAntes, depois)
	}
}

// TestTranscricaoIntegraAusenteNaoDisparaDownload: a íntegra é DERIVADA da legenda. Se faltar
// (falha na geração, limpeza manual), regenerar é a resposta — baixar 3 s de legenda de novo
// seria pagar rede por um arquivo que sai de um arquivo que já está em disco.
func TestTranscricaoIntegraAusenteNaoDisparaDownload(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)
	bf := s.baixador.(*baixadorFake)

	dirVideo, _ := s.cache.DirVideo("cultoTeste1")
	os.MkdirAll(dirVideo, 0755)
	if err := os.WriteFile(filepath.Join(dirVideo, "legenda.srt"),
		[]byte(srtDeTranscricao(transcricaoLongaDoCulto())), 0644); err != nil {
		t.Fatal(err)
	}
	// (sem transcricao.txt: é o estado que se quer exercitar)

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	bf.mu.Lock()
	chamadas := bf.chamadas
	bf.mu.Unlock()
	if chamadas != 0 {
		t.Errorf("baixou a legenda %d vez(es): ela já estava no cache, e a íntegra que faltava é "+
			"derivada dela", chamadas)
	}
	if _, err := os.Stat(filepath.Join(dirVideo, "transcricao.txt")); err != nil {
		t.Errorf("a íntegra não foi regenerada: %v", err)
	}
}

// TestLinhaDoCSVDeRetomadaNaoTemLixo conserta o instrumento antes de acumular mais dado.
//
// A linha de um pedido RETOMADO saía com `quando` em 0001-01-01 e `candidatos` em 0: a
// retomada criava métricas sem data e sem o que já estava em disco. É o CSV com que se mediu o
// ganho do cache — e é o mesmo argumento do viés de amostra do cortes.csv: quem for ler
// precisa poder distinguir o ciclo que PULOU etapas do ciclo que as fez rápido.
func TestLinhaDoCSVDeRetomadaNaoTemLixo(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)

	// Ciclo completo, para haver pedido em disco para retomar.
	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:10:00", "00:30:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id, []int{0})
	esperarStatus(t, s, id, pipeline.EstadoConcluido)

	// Servidor novo, retomando (é o caminho que produzia a linha suja).
	csv := filepath.Join(t.TempDir(), "tempos.csv")
	s2 := Novo(Opcoes{
		Baixador:       &baixadorFake{transc: transcricaoLongaDoCulto(), base: s.baseDir},
		Selecionador:   candsJanela(),
		BaixadorVideo:  &baixadorVideoFake{},
		Renderizador:   &renderFake{outDir: s.outDir},
		BaseDir:        s.baseDir,
		VideosDir:      s.cache.Dir,
		OutDir:         s.outDir,
		LogRodadasPath: filepath.Join(s.baseDir, "rodadas2.md"),
		TemposPath:     csv,
		CortesPath:     filepath.Join(s.baseDir, "cortes2.csv"),
		AcoesPath:      filepath.Join(s.baseDir, "acoes2.csv"),
		Agora:          func() time.Time { return time.Date(2026, 7, 29, 15, 30, 0, 0, time.UTC) },
		GerarID:        func() string { return "nao-usado" },
	})
	if err := s2.Retomar(id); err != nil {
		t.Fatal(err)
	}
	aprovarJSON(t, s2, id, []int{0})
	esperarStatus(t, s2, id, pipeline.EstadoConcluido)
	esperarArquivo(t, csv)

	b, err := os.ReadFile(csv)
	if err != nil {
		t.Fatal(err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava cabeçalho + 1 linha, veio %d: %s", len(linhas), b)
	}
	cab, linha := strings.Split(linhas[0], ","), strings.Split(linhas[1], ",")
	campo := func(nome string) string {
		for i, c := range cab {
			if c == nome && i < len(linha) {
				return linha[i]
			}
		}
		t.Fatalf("coluna %q não existe no cabeçalho: %v", nome, cab)
		return ""
	}

	if strings.HasPrefix(campo("quando"), "0001") {
		t.Errorf("a data saiu zerada (%s): a análise não teria como ordenar nem filtrar por "+
			"período", campo("quando"))
	}
	if campo("candidatos") == "0" {
		t.Error("candidatos = 0 numa retomada que tinha candidatos em disco: é lixo, não dado")
	}
	if campo("sermao_s") == "0" {
		t.Error("sermao_s = 0: é o principal previsor de custo do pedido")
	}
	// A MARCA que permite filtrar: sem ela, uma média de selecionar_s misturaria este ciclo
	// (que nunca selecionou) com os que selecionaram.
	if campo("retomado") != "sim" {
		t.Errorf("retomado = %q, quero sim", campo("retomado"))
	}
	if campo("selecionar_s") != "0.0" {
		t.Errorf("selecionar_s = %s: a retomada pula a seleção, e é isso que a coluna retomado "+
			"explica", campo("selecionar_s"))
	}
	if campo("completou") != "sim" {
		t.Errorf("completou = %q, quero sim", campo("completou"))
	}
}

// TestCSVAntigoGanhaOCabecalhoNovoAlinhado: o cabeçalho é escrito uma vez, na criação. Quando
// a coluna `retomado` entrou, os arquivos existentes ficaram com 20 nomes recebendo linhas de
// 21 campos — desalinhados EM SILÊNCIO. Aconteceu de verdade e só apareceu ao olhar o CSV
// depois de uma medição.
func TestCSVAntigoGanhaOCabecalhoNovoAlinhado(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "tempos.csv")
	// Um CSV da versão anterior: cabeçalho sem a ÚLTIMA coluna (`retomado`) e uma linha com o
	// número de campos daquele cabeçalho. Derivado do cabeçalho atual em vez de escrito à mão,
	// para o teste continuar valendo quando a próxima coluna entrar.
	antigo := strings.TrimRight(cabecalhoTempos, "\n")
	antigo = antigo[:strings.LastIndex(antigo, ",")] + "\n"
	nCampos := strings.Count(antigo, ",") + 1
	linhaVelha := strings.TrimRight(strings.Repeat("x,", nCampos), ",")
	if err := os.WriteFile(csv, []byte(antigo+linhaVelha+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := &Servidor{temposPath: csv}
	s.gravarTempos(&Metricas{ID: "novo", Completou: true, Retomado: true})

	b, err := os.ReadFile(csv)
	if err != nil {
		t.Fatal(err)
	}
	linhas := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(linhas) != 3 {
		t.Fatalf("esperava cabeçalho + linha antiga + linha nova, veio %d:\n%s", len(linhas), b)
	}
	if linhas[0] != strings.TrimRight(cabecalhoTempos, "\n") {
		t.Errorf("o cabeçalho não foi atualizado:\n%s", linhas[0])
	}
	// TODAS as linhas com o mesmo número de campos do cabeçalho: é isso que "alinhado"
	// significa, e é o que uma planilha ou um pandas exige para não errar de coluna.
	nCab := strings.Count(linhas[0], ",")
	for i, l := range linhas[1:] {
		if n := strings.Count(l, ","); n != nCab {
			t.Errorf("linha %d com %d vírgulas, cabeçalho tem %d — desalinhada:\n%s", i+1, n, nCab, l)
		}
	}
	// A linha antiga não ganhou um valor inventado: o campo novo fica VAZIO, porque para ela é
	// desconhecido. Escrever "nao" seria afirmar algo que ninguém mediu.
	if !strings.HasSuffix(linhas[1], ",") {
		t.Errorf("a linha antiga devia terminar com campo vazio, veio:\n%s", linhas[1])
	}
}

// TestAlinhamentoRespeitaAspasELinhaJaNova cobre os dois casos que fizeram a primeira versão
// desta migração ERRAR — e errar consertando, que é o pior lugar para errar:
//
//  1. a coluna `erro` é texto livre e vai entre aspas quando tem vírgula. Contando vírgulas,
//     essas linhas parecem ter campos a mais e não seriam completadas;
//  2. uma linha já no formato NOVO pode conviver com cabeçalho antigo (foi o que aconteceu no
//     arquivo real). Completando cegamente, ela ganhava uma vírgula sobrando.
func TestAlinhamentoRespeitaAspasELinhaJaNova(t *testing.T) {
	csvPath := filepath.Join(t.TempDir(), "tempos.csv")
	atual := strings.TrimRight(cabecalhoTempos, "\n")
	antigo := atual[:strings.LastIndex(atual, ",")]
	nAntigo := strings.Count(antigo, ",") + 1
	nAtual := strings.Count(atual, ",") + 1

	// (a) linha antiga com erro ENTRE ASPAS contendo vírgulas.
	comAspas := make([]string, nAntigo)
	for i := range comAspas {
		comAspas[i] = "x"
	}
	comAspas[nAntigo-1] = `"falhou: a, b, c"`
	// (b) linha já no formato novo (um campo a mais que o cabeçalho antigo).
	jaNova := make([]string, nAtual)
	for i := range jaNova {
		jaNova[i] = "y"
	}
	conteudo := antigo + "\n" + strings.Join(comAspas, ",") + "\n" + strings.Join(jaNova, ",") + "\n"
	if err := os.WriteFile(csvPath, []byte(conteudo), 0644); err != nil {
		t.Fatal(err)
	}

	s := &Servidor{temposPath: csvPath}
	s.gravarTempos(&Metricas{ID: "terceiro", Completou: true})

	reg, err := lerCSV(t, csvPath)
	if err != nil {
		t.Fatalf("o arquivo migrado não é CSV válido: %v", err)
	}
	if len(reg) != 4 {
		t.Fatalf("esperava cabeçalho + 3 linhas, veio %d", len(reg))
	}
	for i, linha := range reg {
		if len(linha) != nAtual {
			t.Errorf("linha %d com %d campos, cabeçalho tem %d: %v", i, len(linha), nAtual, linha)
		}
	}
	// O erro com vírgulas sobreviveu inteiro (não foi partido em vários campos).
	if got := reg[1][nAntigo-1]; got != "falhou: a, b, c" {
		t.Errorf("o campo de erro entre aspas foi corrompido: %q", got)
	}
	// A linha que já estava no formato novo não ganhou campo a mais.
	if reg[2][nAtual-1] != "y" {
		t.Errorf("a linha já no formato novo foi alterada: %v", reg[2])
	}
}

func lerCSV(t *testing.T, path string) ([][]string, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // lê como está; o teste é que verifica se ficou alinhado
	return r.ReadAll()
}

// TestCabecalhoIncompativelNaoEReescrito: se o cabeçalho não for uma versão anterior (coluna
// renomeada, removida, reordenada), adivinhar o alinhamento estragaria o histórico. Avisar e
// não tocar é a resposta.
func TestCabecalhoIncompativelNaoEReescrito(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "tempos.csv")
	estranho := "outra,coisa,completamente,diferente\n1,2,3,4\n"
	if err := os.WriteFile(csv, []byte(estranho), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Servidor{temposPath: csv}
	s.gravarTempos(&Metricas{ID: "novo", Completou: true})

	b, _ := os.ReadFile(csv)
	if !strings.HasPrefix(string(b), estranho) {
		t.Errorf("o arquivo incompatível foi reescrito; devia ficar intacto:\n%s", b)
	}
}

func modificadoEm(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
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
		AcoesPath:      filepath.Join(s.baseDir, "acoes.csv"),
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
