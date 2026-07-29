package retencao

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// criarPedido monta um pedido com os arquivos dados (nome -> tamanho em bytes) e ajusta a
// data de modificação da pasta (para a ordem por recência ser determinística).
func criarPedido(t *testing.T, raiz, id string, idade time.Duration, arquivos map[string]int) string {
	t.Helper()
	dir := filepath.Join(raiz, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	quando := time.Now().Add(-idade)
	for nome, tam := range arquivos {
		p := filepath.Join(dir, nome)
		if err := os.WriteFile(p, make([]byte, tam), 0644); err != nil {
			t.Fatal(err)
		}
		// Envelhece os ARQUIVOS também: a recência do pedido vem do preservado mais
		// recente (não do mtime da pasta, que a própria limpeza altera).
		if err := os.Chtimes(p, quando, quando); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, quando, quando); err != nil {
		t.Fatal(err)
	}
	return dir
}

func pedidoCompleto() map[string]int {
	return map[string]int{
		// bruto (deve sumir)
		"video.mp4":              1000,
		"legenda.srt":            100,
		"legenda.info.json":      50,
		"short_01.sub001.txt":    10,
		"short_01.sub002.txt":    10,
		"mapa.json":              20,
		"candidatos_brutos.json": 20,
		// preservado (deve ficar)
		"candidatos.corrigido.json": 200,
		"transcricao.txt":           300,
		"pedido.json":               40,
		"revisao-teologica.json":    60,
	}
}

func existe(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// --- Política de retenção ---

func TestRetemOsMaisRecentesELimpaOsAntigos(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "novo", 1*time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "medio", 1*time.Hour, pedidoCompleto())
	criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	res, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1})
	if err != nil {
		t.Fatal(err)
	}
	// O mais recente fica intacto; os dois antigos perdem o bruto.
	if len(res.Retidos) != 1 || res.Retidos[0] != "novo" {
		t.Errorf("retidos = %v, quero [novo]", res.Retidos)
	}
	if !existe(t, filepath.Join(raiz, "novo", "video.mp4")) {
		t.Error("o pedido mais recente NÃO deveria ter sido limpo")
	}
	for _, id := range []string{"medio", "antigo"} {
		if existe(t, filepath.Join(raiz, id, "video.mp4")) {
			t.Errorf("%s: video.mp4 deveria ter sido apagado", id)
		}
	}
	if len(res.Pedidos) != 2 {
		t.Errorf("esperava 2 pedidos limpos, veio %d", len(res.Pedidos))
	}
}

func TestReterConfiguravel(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "p1", 1*time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "p2", 1*time.Hour, pedidoCompleto())
	criarPedido(t, raiz, "p3", 24*time.Hour, pedidoCompleto())

	res, _ := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 2})
	if len(res.Retidos) != 2 {
		t.Errorf("com Reter=2 esperava 2 retidos, veio %v", res.Retidos)
	}
	if !existe(t, filepath.Join(raiz, "p2", "video.mp4")) {
		t.Error("p2 (2º mais recente) deveria ter sido retido")
	}
	if existe(t, filepath.Join(raiz, "p3", "video.mp4")) {
		t.Error("p3 (mais antigo) deveria ter sido limpo")
	}
}

func TestReterZeroViraUm(t *testing.T) {
	// Reter=0 nunca pode limpar TUDO: perder o último bruto obriga a baixar de novo.
	raiz := t.TempDir()
	criarPedido(t, raiz, "unico", time.Minute, pedidoCompleto())
	Limpar(Opcoes{RaizTrabalho: raiz, Reter: 0})
	if !existe(t, filepath.Join(raiz, "unico", "video.mp4")) {
		t.Error("Reter=0 deveria virar 1 e preservar o pedido mais recente")
	}
}

func TestIntocavelNuncaELimpo(t *testing.T) {
	// O pedido EM CURSO não pode ser limpo no meio da execução.
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "em-curso", 48*time.Hour, pedidoCompleto()) // antigo, mas em uso

	Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1, Intocaveis: []string{"em-curso"}})
	if !existe(t, filepath.Join(raiz, "em-curso", "video.mp4")) {
		t.Error("pedido intocável (em curso) foi limpo")
	}
}

// --- Whitelist de preservação (a trava contra erro humano) ---

// Este teste FALHA se alguém adicionar um arquivo protegido à lista de remoção — que é
// exatamente o erro que apagaria o histórico auditável sem ninguém perceber.
func TestPreservadosNuncaSaoRemoviveis(t *testing.T) {
	for _, nome := range preservados {
		if PodeRemover(nome) {
			t.Errorf("PERIGO: %q está marcado como removível — é histórico auditável", nome)
		}
	}
	// Nomes que JAMAIS podem ser removidos, escritos aqui de forma independente das
	// listas: se alguém renomear/remover um item de `preservados`, este teste pega.
	for _, nome := range []string{
		"candidatos.corrigido.json", // fonte de verdade validada (spec-09)
		"transcricao.txt",           // insumo de auditoria (spec-16)
		"revisao-teologica.json",    // veredito do confronto (spec-14)
		"pedido.json",               // metadados do pedido
		// legenda.srt: 281 KB medidos, e desde o cache por vídeo ela é do CULTO. Apagá-la fazia
		// o próximo pedido do mesmo culto rebaixá-la à toa — esvaziando o cache por uma regra
		// anterior a ele.
		"legenda.srt",
	} {
		if PodeRemover(nome) {
			t.Errorf("PERIGO: %q seria apagado pela limpeza", nome)
		}
	}
	// E o inverso: o bruto realmente é removível (senão a limpeza não faz nada).
	for _, nome := range []string{"video.mp4", "legenda.info.json", "short_03.sub012.txt", "video.mp4.part"} {
		if !PodeRemover(nome) {
			t.Errorf("%q deveria ser removível (é bruto regenerável)", nome)
		}
	}
	// Arquivo desconhecido: na dúvida, NÃO apaga.
	for _, nome := range []string{"anotacoes.md", "short_01.mp4", "qualquer.coisa"} {
		if PodeRemover(nome) {
			t.Errorf("%q é desconhecido — a limpeza deveria deixar quieto", nome)
		}
	}
}

func TestLimpezaPreservaArquivosDoHistorico(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	dir := criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	if _, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1}); err != nil {
		t.Fatal(err)
	}
	for _, nome := range preservados {
		if !existe(t, filepath.Join(dir, nome)) {
			t.Errorf("arquivo preservado foi apagado: %s", nome)
		}
	}
}

// A limpeza opera SOMENTE dentro da raiz de trabalho — finalizados/ é intocável por não
// estar lá dentro. Este teste garante que uma pasta irmã não é afetada.
func TestNuncaTocaForaDaRaiz(t *testing.T) {
	tmp := t.TempDir()
	raiz := filepath.Join(tmp, "trabalho")
	finalizados := filepath.Join(tmp, "finalizados", "antigo")
	os.MkdirAll(finalizados, 0755)
	short := filepath.Join(finalizados, "short_01.mp4")
	os.WriteFile(short, []byte("entregue"), 0644)
	// Um video.mp4 em finalizados/ (nome que a limpeza remove DENTRO de trabalho/).
	os.WriteFile(filepath.Join(finalizados, "video.mp4"), []byte("x"), 0644)

	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	if _, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1}); err != nil {
		t.Fatal(err)
	}
	if !existe(t, short) {
		t.Error("PERIGO: a limpeza apagou um Short entregue em finalizados/")
	}
	if !existe(t, filepath.Join(finalizados, "video.mp4")) {
		t.Error("PERIGO: a limpeza tocou em arquivo fora da raiz de trabalho")
	}
}

func TestCaminhoSeguroRecusaTravessia(t *testing.T) {
	raiz := t.TempDir()
	for _, id := range []string{"..", "../..", "../finalizados", "a/b", "/etc", "", "."} {
		if _, err := caminhoSeguro(raiz, id); err == nil {
			t.Errorf("caminhoSeguro aceitou id perigoso: %q", id)
		}
	}
	if _, err := caminhoSeguro(raiz, "pedido-valido"); err != nil {
		t.Errorf("caminhoSeguro recusou id válido: %v", err)
	}
}

// --- dry-run ---

func TestDryRunNaoApagaNada(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	dir := criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	res, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	// Relata o que faria...
	if len(res.Pedidos) != 1 || res.BytesLiberados == 0 {
		t.Errorf("dry-run deveria relatar o que faria: %+v", res)
	}
	if !strings.Contains(res.Resumo(), "dry-run") {
		t.Errorf("resumo do dry-run deveria dizer isso: %q", res.Resumo())
	}
	// ...mas NÃO apaga.
	if !existe(t, filepath.Join(dir, "video.mp4")) {
		t.Error("dry-run APAGOU o arquivo — é exatamente o que não pode acontecer")
	}
}

func TestIdempotenteEContaBytes(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	res1, _ := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1})
	// bruto do pedido antigo, sem a legenda.srt (100 B), que passou a ser preservada:
	// 1000+50+10+10+20+20 = 1110
	if res1.BytesLiberados != 1110 {
		t.Errorf("bytes liberados = %d, quero 1110", res1.BytesLiberados)
	}
	// Rodar de novo não quebra nem "libera" de novo.
	res2, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1})
	if err != nil {
		t.Fatalf("segunda limpeza falhou: %v", err)
	}
	if res2.BytesLiberados != 0 {
		t.Errorf("segunda limpeza deveria não achar nada, liberou %d", res2.BytesLiberados)
	}
}

// Regressão: apagar arquivos ATUALIZA o mtime da pasta. Se a recência viesse dali, o
// pedido recém-limpo viraria "o mais recente" e a limpeza seguinte comeria justamente o
// pedido que a política manda reter — perdendo o bruto que serve para regerar sem baixar
// de novo. A recência vem dos arquivos PRESERVADOS, que a limpeza não toca.
func TestLimpezasRepetidasNaoComemOPedidoRetido(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	for i := 1; i <= 3; i++ {
		if _, err := Limpar(Opcoes{RaizTrabalho: raiz, Reter: 1}); err != nil {
			t.Fatalf("limpeza %d: %v", i, err)
		}
		if !existe(t, filepath.Join(raiz, "recente", "video.mp4")) {
			t.Fatalf("limpeza %d apagou o bruto do pedido que deveria reter", i)
		}
	}
}

func TestRaizInexistenteNaoErra(t *testing.T) {
	res, err := Limpar(Opcoes{RaizTrabalho: filepath.Join(t.TempDir(), "nao-existe")})
	if err != nil {
		t.Errorf("raiz inexistente não deveria dar erro: %v", err)
	}
	if len(res.Pedidos) != 0 {
		t.Error("nada a limpar numa raiz inexistente")
	}
}

// --- Resíduo de pedidos que falharam ---

// Um download que morre deixa mp4 parcial e os temporários do yt-dlp. Como falha costuma
// acontecer com o disco apertado, esse lixo PRECISA ser removível — senão as falhas
// acumulam e o problema se realimenta.
func TestResiduoDeFalhaEhRemovivel(t *testing.T) {
	for _, nome := range []string{"video.mp4.part", "video.f398.mp4.part", "video.mp4.ytdl", "video.part"} {
		if !PodeRemover(nome) {
			t.Errorf("%q é resíduo de download interrompido e deveria ser removível", nome)
		}
	}
}

// LimparPedido apaga o bruto de UM pedido, ignorando a política de retenção — é o caminho
// do pedido que falhou (não tem Short a regerar), mesmo sendo o mais recente.
func TestLimparPedidoIgnoraPoliticaMasPreservaHistorico(t *testing.T) {
	raiz := t.TempDir()
	arquivos := pedidoCompleto()
	arquivos["video.mp4.part"] = 700 // download interrompido
	dir := criarPedido(t, raiz, "falhou", time.Minute, arquivos)

	p, err := LimparPedido(raiz, "falhou", false)
	if err != nil {
		t.Fatal(err)
	}
	if existe(t, filepath.Join(dir, "video.mp4")) || existe(t, filepath.Join(dir, "video.mp4.part")) {
		t.Error("o bruto (inclusive o .part) do pedido que falhou deveria ter sido removido")
	}
	// O histórico continua — mesmo num pedido que falhou, o que existir é auditável.
	for _, nome := range preservados {
		if !existe(t, filepath.Join(dir, nome)) {
			t.Errorf("preservado foi apagado: %s", nome)
		}
	}
	if p.Bytes == 0 {
		t.Error("deveria reportar os bytes liberados")
	}
}

func TestLimparPedidoRecusaCaminhoPerigoso(t *testing.T) {
	raiz := t.TempDir()
	if _, err := LimparPedido(raiz, "../outro", false); err == nil {
		t.Error("LimparPedido deveria recusar travessia de caminho")
	}
}

// --- Verificação de espaço ANTES de baixar ---

func TestGarantirEspacoPassaComMargemFolgada(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "p", time.Minute, pedidoCompleto())
	// Margem de 1 byte: qualquer disco tem isso.
	if _, err := GarantirEspaco(Opcoes{RaizTrabalho: raiz, Reter: 1}, 1); err != nil {
		t.Errorf("com margem trivial não deveria falhar: %v", err)
	}
}

// A falha tem que vir ANTES do download e NOMEAR os números — "espaço insuficiente: N
// livres, precisa de ~M". Falhar no meio do download dá um erro de biblioteca do yt-dlp,
// que não diz nada ao operador.
func TestGarantirEspacoFalhaComMensagemUtil(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "p", time.Minute, pedidoCompleto())
	// Margem absurda: nenhum disco tem 1 EB livre.
	_, err := GarantirEspaco(Opcoes{RaizTrabalho: raiz, Reter: 1}, 1<<60)
	if err == nil {
		t.Fatal("deveria falhar por espaço insuficiente")
	}
	if !errors.Is(err, ErrEspacoInsuficiente) {
		t.Errorf("erro deveria ser ErrEspacoInsuficiente, veio: %v", err)
	}
	for _, q := range []string{"livres", "precisa"} {
		if !strings.Contains(err.Error(), q) {
			t.Errorf("mensagem não informa os números (%q): %v", q, err)
		}
	}
}

// Antes de desistir, a verificação TENTA liberar espaço com a limpeza.
func TestGarantirEspacoTentaLimparAntesDeFalhar(t *testing.T) {
	raiz := t.TempDir()
	criarPedido(t, raiz, "recente", time.Minute, pedidoCompleto())
	dir := criarPedido(t, raiz, "antigo", 24*time.Hour, pedidoCompleto())

	// Margem impossível: falha, mas a limpeza deve ter rodado no caminho.
	GarantirEspaco(Opcoes{RaizTrabalho: raiz, Reter: 1}, 1<<60)
	if existe(t, filepath.Join(dir, "video.mp4")) {
		t.Error("a verificação deveria ter tentado a limpeza antes de desistir")
	}
}

func TestEspacoLivreRetornaValorPlausivel(t *testing.T) {
	livre, err := EspacoLivre(t.TempDir())
	if err != nil {
		t.Skipf("sistema sem suporte a consulta de espaço: %v", err)
	}
	if livre <= 0 {
		t.Errorf("espaço livre = %d, esperava um valor positivo", livre)
	}
}

func TestFormatarBytes(t *testing.T) {
	casos := map[int64]string{
		1024:                   "1 KB",
		5 * 1024 * 1024:        "5 MB",
		2 * 1024 * 1024 * 1024: "2.0 GB",
	}
	for b, quer := range casos {
		if got := FormatarBytes(b); got != quer {
			t.Errorf("FormatarBytes(%d) = %q, quero %q", b, got, quer)
		}
	}
}
