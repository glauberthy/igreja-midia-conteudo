package retencao

import (
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
	} {
		if PodeRemover(nome) {
			t.Errorf("PERIGO: %q seria apagado pela limpeza", nome)
		}
	}
	// E o inverso: o bruto realmente é removível (senão a limpeza não faz nada).
	for _, nome := range []string{"video.mp4", "legenda.srt", "short_03.sub012.txt", "video.mp4.part"} {
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
	// bruto do pedido antigo: 1000+100+50+10+10+20+20 = 1210
	if res1.BytesLiberados != 1210 {
		t.Errorf("bytes liberados = %d, quero 1210", res1.BytesLiberados)
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
