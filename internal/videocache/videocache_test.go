package videocache

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
)

// legendaExemplo é um SRT com fala espalhada ao longo de 40 minutos: há blocos ANTES, DENTRO e
// DEPOIS de qualquer janela que os testes usem, que é o que permite verificar recorte de fato.
const legendaExemplo = `1
00:04:00,000 --> 00:04:03,000
louvor antes da pregacao

2
00:05:31,000 --> 00:05:34,000
a graca de Deus e suficiente para o dia de hoje

3
00:10:15,000 --> 00:10:18,000
o centuriao disse que bastava uma palavra

4
00:20:40,000 --> 00:20:43,000
por isso descansa e confia no Senhor

5
00:39:10,000 --> 00:39:13,000
avisos e bencao final
`

func cacheDeTeste(t *testing.T) *Cache {
	t.Helper()
	c := Novo(filepath.Join(t.TempDir(), "videos"))
	c.MinBytes = 1 // os "vídeos" dos testes são arquivos pequenos
	c.Agora = func() time.Time { return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC) }
	return c
}

// comLegenda põe a legenda do culto no cache, como o download faria.
func comLegenda(t *testing.T, c *Cache, videoID string) string {
	t.Helper()
	dir, err := c.DirVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, NomeLegenda), []byte(legendaExemplo), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// comVideo põe um "vídeo" no cache e registra a origem, como a fase pesada faria.
func comVideo(t *testing.T, c *Cache, videoID string, origemMs int) {
	t.Helper()
	dir := comLegenda(t, c, videoID)
	if err := os.WriteFile(filepath.Join(dir, NomeVideo), []byte("mp4 de teste"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Registrar(videoID, origemMs, "Culto de Teste"); err != nil {
		t.Fatal(err)
	}
}

// ============================================================================================
// O artefato DERIVADO: a transcrição do pedido
// ============================================================================================

// TestRegenerarORecorteDaMesmosBytes é o teste que fecha o risco da transcrição existir em dois
// lugares (íntegra no cache, recortada no pedido).
//
// As duas cópias só são seguras porque uma é DERIVÁVEL da outra. Isso é uma afirmação, e uma
// afirmação sem teste é uma esperança: aqui ela é verificada BYTE A BYTE. Se um dia o vídeo for
// rebaixado e a legenda do cache mudar, é este teste (rodando sobre o material real) que acusa
// a divergência — em vez de o operador descobrir por um Short com texto de outra janela.
func TestRegenerarORecorteDaMesmosBytes(t *testing.T) {
	c := cacheDeTeste(t)
	comLegenda(t, c, "cultoTeste1")
	destinoA := filepath.Join(t.TempDir(), "pedido-a", "transcricao.txt")
	destinoB := filepath.Join(t.TempDir(), "pedido-b", "transcricao.txt")

	const iniMs, fimMs = 5 * 60 * 1000, 21 * 60 * 1000 // 00:05:00 → 00:21:00

	recA, err := c.DerivarTranscricao("cultoTeste1", destinoA, iniMs, fimMs)
	if err != nil {
		t.Fatalf("primeira derivação: %v", err)
	}
	recB, err := c.DerivarTranscricao("cultoTeste1", destinoB, iniMs, fimMs)
	if err != nil {
		t.Fatalf("segunda derivação: %v", err)
	}

	a, err := os.ReadFile(destinoA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(destinoB)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("regenerar o recorte deu bytes DIFERENTES.\nprimeira:\n%s\nsegunda:\n%s", a, b)
	}
	if len(a) == 0 {
		t.Fatal("o recorte saiu vazio: o teste não provaria nada")
	}

	// E a PROVENIÊNCIA é a mesma, porque é o que o pedido.json guarda para permitir esta
	// própria verificação depois.
	if recA != recB {
		t.Errorf("proveniências diferentes para a mesma derivação: %+v vs %+v", recA, recB)
	}
	if recA.VideoID != "cultoTeste1" || recA.Inicio != "00:05:00" || recA.Fim != "00:21:00" {
		t.Errorf("proveniência inesperada: %+v", recA)
	}

	// O recorte tem de ser recorte MESMO: dentro da janela entra, fora não. Sem isto, um
	// "derivar" que copiasse a íntegra passaria no teste de bytes iguais.
	txt := string(a)
	if !strings.Contains(txt, "a graca de Deus") || !strings.Contains(txt, "o centuriao disse") {
		t.Errorf("faltou fala que está DENTRO da janela:\n%s", txt)
	}
	if strings.Contains(txt, "louvor antes da pregacao") || strings.Contains(txt, "avisos e bencao") {
		t.Errorf("entrou fala de FORA da janela (o recorte não recortou):\n%s", txt)
	}

	// E A METADE QUE IMPORTA DE VERDADE: se a legenda do cache MUDAR (vídeo rebaixado, o
	// YouTube corrigindo a legenda automática), a regeneração deixa de bater com o que está no
	// pedido. É essa comparação que transforma "as duas cópias são consistentes" de esperança
	// em verificação — e é o alarme que o dono pediu para existir.
	dir, _ := c.DirVideo("cultoTeste1")
	novaLegenda := strings.Replace(legendaExemplo, "bastava uma palavra", "bastava uma palavra apenas", 1)
	if err := os.WriteFile(filepath.Join(dir, NomeLegenda), []byte(novaLegenda), 0644); err != nil {
		t.Fatal(err)
	}
	destinoC := filepath.Join(t.TempDir(), "pedido-c", "transcricao.txt")
	if _, err := c.DerivarTranscricao("cultoTeste1", destinoC, iniMs, fimMs); err != nil {
		t.Fatal(err)
	}
	depois, _ := os.ReadFile(destinoC)
	if bytes.Equal(a, depois) {
		t.Error("a legenda do cache mudou e a regeneração deu os MESMOS bytes: então a derivação " +
			"não depende da legenda, e a proveniência gravada no pedido não garante nada")
	}
}

// TestJanelasDiferentesDaoRecortesDiferentes: é o caso que motiva o cache guardar a ÍNTEGRA.
// Se a legenda do cache já viesse recortada (como o download fazia antes), o segundo pedido
// receberia o recorte do primeiro.
func TestJanelasDiferentesDaoRecortesDiferentes(t *testing.T) {
	c := cacheDeTeste(t)
	comLegenda(t, c, "cultoTeste1")
	dir := t.TempDir()

	primeiro := filepath.Join(dir, "p1.txt")
	segundo := filepath.Join(dir, "p2.txt")
	if _, err := c.DerivarTranscricao("cultoTeste1", primeiro, 5*60*1000, 12*60*1000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DerivarTranscricao("cultoTeste1", segundo, 20*60*1000, 40*60*1000); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(primeiro)
	b, _ := os.ReadFile(segundo)
	if bytes.Equal(a, b) {
		t.Fatal("as duas janelas deram o mesmo texto: o cache está servindo recorte, não a íntegra")
	}
	if !strings.Contains(string(a), "centuriao") || strings.Contains(string(a), "descansa e confia") {
		t.Errorf("janela 1 errada:\n%s", a)
	}
	if !strings.Contains(string(b), "descansa e confia") || strings.Contains(string(b), "centuriao") {
		t.Errorf("janela 2 errada:\n%s", b)
	}
}

// TestIntegraEAMesmaDerivacaoComAJanelaToda: a íntegra do cache não é um terceiro dado com
// regra própria — é a mesma função com a janela inteira.
func TestIntegraEAMesmaDerivacaoComAJanelaToda(t *testing.T) {
	c := cacheDeTeste(t)
	comLegenda(t, c, "cultoTeste1")
	if err := c.GerarTranscricaoIntegra("cultoTeste1"); err != nil {
		t.Fatal(err)
	}
	dir, _ := c.DirVideo("cultoTeste1")
	integra, err := os.ReadFile(filepath.Join(dir, NomeTransc))
	if err != nil {
		t.Fatal(err)
	}
	esperado := filepath.Join(t.TempDir(), "tudo.txt")
	if _, err := c.DerivarTranscricao("cultoTeste1", esperado, 0, JanelaInteira); err != nil {
		t.Fatal(err)
	}
	querBytes, _ := os.ReadFile(esperado)
	if !bytes.Equal(integra, querBytes) {
		t.Errorf("a íntegra não é a derivação com janela inteira:\n%s\n---\n%s", integra, querBytes)
	}
	// E ela contém o culto TODO, inclusive o que está fora de qualquer janela de pregação.
	if !strings.Contains(string(integra), "louvor antes") || !strings.Contains(string(integra), "bencao final") {
		t.Errorf("a íntegra não tem o culto inteiro:\n%s", integra)
	}
}

// ============================================================================================
// O RESOLVEDOR
// ============================================================================================

func TestLocalizarPrecedenciaPedidoVenceCache(t *testing.T) {
	c := cacheDeTeste(t)
	base := t.TempDir()
	comVideo(t, c, "cultoTeste1", OrigemVideoInteiro)

	// O MESMO pedido também tem vídeo na própria pasta, baixado por JANELA pelo cmd/baixar.
	dirPedido := filepath.Join(base, "p1")
	os.MkdirAll(dirPedido, 0755)
	os.WriteFile(filepath.Join(dirPedido, NomeVideo), []byte("janela"), 0644)

	ped := &pipeline.Pedido{ID: "p1", VideoID: "cultoTeste1", Inicio: "00:05:00"}
	ped.DeclararOrigem(300000) // 00:05:00 — é uma janela

	fonte, err := c.Localizar(base, ped)
	if err != nil {
		t.Fatalf("Localizar: %v", err)
	}
	if fonte.DoCache {
		t.Error("o vídeo da pasta do pedido tem de vencer o do cache: é o mais específico")
	}
	if fonte.OrigemMs != 300000 {
		t.Errorf("origem = %d, quero 300000 (a do pedido, não a do cache)", fonte.OrigemMs)
	}
	if fonte.Path != filepath.Join(dirPedido, NomeVideo) {
		t.Errorf("caminho = %q, quero o da pasta do pedido", fonte.Path)
	}
}

func TestLocalizarUsaOCacheQuandoOPedidoNaoTemVideo(t *testing.T) {
	c := cacheDeTeste(t)
	base := t.TempDir()
	comVideo(t, c, "cultoTeste1", OrigemVideoInteiro)

	ped := &pipeline.Pedido{ID: "p1", VideoID: "cultoTeste1", Inicio: "00:49:15"}
	// Sem origem declarada NO PEDIDO — de propósito: a do cache é que vale, e ela vive no
	// video.json. Um pedido que aponta para o cache não precisa declarar nada.
	fonte, err := c.Localizar(base, ped)
	if err != nil {
		t.Fatalf("Localizar: %v", err)
	}
	if !fonte.DoCache {
		t.Error("devia ter resolvido pelo cache")
	}
	if fonte.OrigemMs != 0 {
		t.Errorf("origem = %d, quero 0 (o vídeo do cache é o INTEIRO) — e note que ped.Inicio é "+
			"00:49:15: se a origem viesse dele, o corte sairia 49 min fora de lugar", fonte.OrigemMs)
	}
}

// TestLocalizarSemOrigemDeclaradaFalhaClaro é a guarda que veio do internal/video: vídeo na
// pasta do pedido SEM declaração de origem não renderiza com um padrão silencioso.
func TestLocalizarSemOrigemDeclaradaFalhaClaro(t *testing.T) {
	c := cacheDeTeste(t)
	base := t.TempDir()
	dirPedido := filepath.Join(base, "p1")
	os.MkdirAll(dirPedido, 0755)
	os.WriteFile(filepath.Join(dirPedido, NomeVideo), []byte("video sem declaracao"), 0644)

	ped := &pipeline.Pedido{ID: "p1", VideoID: "cultoTeste1", Inicio: "00:49:15"}
	_, err := c.Localizar(base, ped)
	if err == nil {
		t.Fatal("devia falhar: sem declaração não se sabe a que instante o arquivo corresponde")
	}
	for _, quero := range []string{"origem_ms", "pedido.json", "vídeo inteiro"} {
		if !strings.Contains(err.Error(), quero) {
			t.Errorf("a mensagem não menciona %q (quem topa com ela é o operador): %v", quero, err)
		}
	}
}

func TestLocalizarSemVideoNenhumFalhaDizendoOQueFalta(t *testing.T) {
	c := cacheDeTeste(t)
	base := t.TempDir()
	ped := &pipeline.Pedido{ID: "p1", VideoID: "cultoTeste1"}

	_, err := c.Localizar(base, ped)
	if err == nil {
		t.Fatal("sem vídeo em lugar nenhum devia ser erro")
	}
	// A mensagem cita OS DOIS caminhos procurados: é o que permite ao operador agir sem ler
	// código.
	if !strings.Contains(err.Error(), filepath.Join("p1", NomeVideo)) ||
		!strings.Contains(err.Error(), filepath.Join("cultoTeste1", NomeVideo)) {
		t.Errorf("a mensagem não diz onde procurou: %v", err)
	}
}

func TestLocalizarSemVideoIDExplica(t *testing.T) {
	c := cacheDeTeste(t)
	ped := &pipeline.Pedido{ID: "p1"} // pedido antigo, sem video_id
	_, err := c.Localizar(t.TempDir(), ped)
	if err == nil || !strings.Contains(err.Error(), "video_id") {
		t.Errorf("sem video_id a mensagem tem de dizer isso: %v", err)
	}
}

// TestVideoNoCacheSemIndiceNaoEUsadoAsCegas: o arquivo existir não basta — sem o video.json
// ninguém sabe a origem dele, e usar "0 porque provavelmente é o inteiro" é exatamente a
// suposição que produziu o bug.
func TestVideoNoCacheSemIndiceNaoEUsadoAsCegas(t *testing.T) {
	c := cacheDeTeste(t)
	dir := comLegenda(t, c, "cultoTeste1")
	os.WriteFile(filepath.Join(dir, NomeVideo), []byte("mp4 sem indice"), 0644)
	// (sem Registrar: não há video.json)

	ped := &pipeline.Pedido{ID: "p1", VideoID: "cultoTeste1"}
	_, err := c.Localizar(t.TempDir(), ped)
	if err == nil {
		t.Fatal("vídeo no cache sem video.json devia falhar, não assumir origem 0")
	}
	if !strings.Contains(err.Error(), "video.json") || !strings.Contains(err.Error(), "origem") {
		t.Errorf("a mensagem devia apontar o video.json ausente: %v", err)
	}
}

// ============================================================================================
// O CACHE
// ============================================================================================

func TestRegistrarETocarMantemAsDatas(t *testing.T) {
	c := cacheDeTeste(t)
	comVideo(t, c, "cultoTeste1", OrigemVideoInteiro)

	idx, err := c.LerIndice("cultoTeste1")
	if err != nil {
		t.Fatal(err)
	}
	if idx.VideoID != "cultoTeste1" || idx.OrigemMs != 0 || idx.Titulo != "Culto de Teste" {
		t.Errorf("índice inesperado: %+v", idx)
	}
	if idx.Bytes == 0 {
		t.Error("índice sem tamanho: a expiração por teto precisa dele")
	}
	baixado := idx.BaixadoEm

	// Uma semana depois, outro pedido reaproveita o vídeo.
	c.Agora = func() time.Time { return baixado.Add(7 * 24 * time.Hour) }
	if err := c.Tocar("cultoTeste1"); err != nil {
		t.Fatal(err)
	}
	idx2, _ := c.LerIndice("cultoTeste1")
	if !idx2.BaixadoEm.Equal(baixado) {
		t.Error("Tocar não pode mexer no baixado_em: é o registro de quando o arquivo entrou")
	}
	if !idx2.UsadoEm.After(baixado) {
		t.Errorf("usado_em não avançou (%v): é ele que protege da expiração o culto reprocessado",
			idx2.UsadoEm)
	}
	if idx2.OrigemMs != idx.OrigemMs || idx2.Titulo != idx.Titulo {
		t.Errorf("Tocar corrompeu o resto do índice: %+v", idx2)
	}
}

// TestCacheRecusaVideoDeJanela é a INVARIANTE do pacote: aqui só entra vídeo inteiro.
//
// Antes esta regra vivia na migração — um caminho. Qualquer via nova de escrita no cache
// reabriria o furo, e o furo é grave de um jeito diferente do bug que já pagamos: um vídeo de
// janela com `origem_ms: 0` no video.json é a mentira GRAVADA EM DISCO, servindo todo pedido
// futuro que reusar aquele culto — inclusive de outro sermão. O bug de corte deslocado morria
// no fim da execução; este se espalha.
//
// Verificado onde importa: no Registrar, que é por onde qualquer escritor passa.
func TestCacheRecusaVideoDeJanela(t *testing.T) {
	c := cacheDeTeste(t)
	comLegenda(t, c, "cultoTeste1")

	// Uma janela de pregação começando em 00:49:15 — o caso real do cmd/baixar.
	const origemDeJanela = 2955000
	err := c.Registrar("cultoTeste1", origemDeJanela, "Culto")
	if err == nil {
		t.Fatal("o cache aceitou um vídeo de JANELA: a origem declarada passaria a mentir sobre " +
			"o conteúdo, em disco, para todos os pedidos que reusarem o culto")
	}
	if !errors.Is(err, ErrOrigemNaoZero) {
		t.Errorf("erro devia ser ErrOrigemNaoZero (para quem chama poder distinguir): %v", err)
	}
	// A mensagem tem de explicar a consequência, não só dizer "inválido".
	for _, quero := range []string{"JANELA", "mentiria", "DISCO"} {
		if !strings.Contains(err.Error(), quero) {
			t.Errorf("a mensagem não menciona %q: %v", quero, err)
		}
	}

	// E NADA foi gravado: recusar é recusar, não gravar e reclamar.
	if _, err := c.LerIndice("cultoTeste1"); err == nil {
		t.Error("o video.json foi gravado apesar da recusa")
	}

	// CONTRAPROVA: o mesmo Registrar aceita o vídeo inteiro. Sem isto, o teste acima passaria
	// também se o Registrar tivesse parado de funcionar por completo.
	if err := c.Registrar("cultoTeste1", OrigemVideoInteiro, "Culto"); err != nil {
		t.Fatalf("o vídeo INTEIRO tem de ser aceito: %v", err)
	}
	if idx, err := c.LerIndice("cultoTeste1"); err != nil || idx.OrigemMs != 0 {
		t.Errorf("índice do vídeo inteiro não gravou direito: %+v (%v)", idx, err)
	}
}

// TestAceitaEARegraUnica: a migração pergunta ANTES de mover o arquivo, e tem de perguntar à
// MESMA regra que o Registrar impõe. Se as duas divergirem, a migração move um arquivo que o
// cache vai recusar — e o vídeo fica fora da pasta do pedido e sem índice no cache.
func TestAceitaEARegraUnica(t *testing.T) {
	c := cacheDeTeste(t)
	comLegenda(t, c, "cultoTeste1")
	for _, origem := range []int{0, 1, 1000, 2955000, -1} {
		porAceita := Aceita(origem) == nil
		porRegistrar := c.Registrar("cultoTeste1", origem, "t") == nil
		if porAceita != porRegistrar {
			t.Errorf("origem %d: Aceita diz %v e Registrar diz %v — as duas checagens divergiram, "+
				"e é essa divergência que faz a migração mover o que o cache recusa",
				origem, porAceita, porRegistrar)
		}
	}
}

func TestTemVideoRecusaRestoDeDownload(t *testing.T) {
	c := Novo(filepath.Join(t.TempDir(), "videos")) // MinBytes de produção (20 MB)
	dir := comLegenda(t, c, "cultoTeste1")
	// 8 MB: é o tamanho de um parcial já observado em produção.
	f, _ := os.Create(filepath.Join(dir, NomeVideo))
	f.Truncate(8 << 20)
	f.Close()

	if c.TemVideo("cultoTeste1") {
		t.Error("8 MB é resto de download interrompido, não vídeo de culto: tratá-lo como vídeo " +
			"produz Short vazio ou erro obscuro do ffmpeg")
	}
}

func TestDirVideoRecusaIDQueEscapaDaRaiz(t *testing.T) {
	c := cacheDeTeste(t)
	for _, id := range []string{"", ".", "..", "../fora", "a/b", "/etc/passwd"} {
		if dir, err := c.DirVideo(id); err == nil {
			t.Errorf("DirVideo(%q) = %q sem erro: o id vira NOME DE PASTA, então tem de ser "+
				"recusado antes de montar caminho", id, dir)
		}
	}
}
