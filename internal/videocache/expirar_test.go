package videocache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A falha que estes testes cobrem é CARA E SILENCIOSA nos dois sentidos: apagar de menos enche
// um disco de 516 GB e derruba o pipeline com um erro que não fala do problema; apagar de mais
// joga fora 570 MB que voltam em 35 s de download — ou, pior, o vídeo de um render em andamento.
// Por isso a expiração tem guarda, e a guarda tem teste com mutação.

const dia = 24 * time.Hour

// cultoNoCache põe um culto no cache com tamanho e último uso escolhidos.
//
// Escreve os bytes de verdade (e não um tamanho fingido) porque o teto é decidido somando o que
// está EM DISCO: um vídeo de mentira com metadado grande faria o teste passar com uma conta que
// a produção não faz.
func cultoNoCache(t *testing.T, c *Cache, videoID string, mb int, usadoEm time.Time) {
	t.Helper()
	dir := comLegenda(t, c, videoID)
	if err := os.WriteFile(filepath.Join(dir, NomeVideo), make([]byte, mb<<20), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, NomeInfo), []byte(`{"id":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Registrar(videoID, OrigemVideoInteiro, "Culto "+videoID); err != nil {
		t.Fatal(err)
	}
	idx, err := c.LerIndice(videoID)
	if err != nil {
		t.Fatal(err)
	}
	idx.UsadoEm = usadoEm
	if err := c.gravarIndice(dir, idx); err != nil {
		t.Fatal(err)
	}
}

func temArquivo(t *testing.T, c *Cache, videoID, nome string) bool {
	t.Helper()
	dir, err := c.DirVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(dir, nome))
	return err == nil
}

// TestExpiraPorPrazo: sem uso há mais de 30 dias, o vídeo sai.
func TestExpiraPorPrazo(t *testing.T) {
	c := cacheDeTeste(t)
	agora := c.agora()
	cultoNoCache(t, c, "cultoVelho1", 1, agora.Add(-31*dia))
	cultoNoCache(t, c, "cultoNovo001", 1, agora.Add(-2*dia))

	res, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 1 || res.Cultos[0].VideoID != "cultoVelho1" {
		t.Fatalf("esperava só o culto velho expirado, veio %+v", res.Cultos)
	}
	if res.Cultos[0].Motivo != MotivoPrazo {
		t.Errorf("motivo = %q, quero %q", res.Cultos[0].Motivo, MotivoPrazo)
	}
	if temArquivo(t, c, "cultoVelho1", NomeVideo) {
		t.Error("o vídeo do culto de 31 dias continua em disco")
	}
	if !temArquivo(t, c, "cultoNovo001", NomeVideo) {
		t.Error("o vídeo de 2 dias foi apagado: a régua do prazo pegou quem não devia")
	}
}

// TestExpiraPorTetoDentroDoPrazo é o caso que o prazo sozinho NÃO cobre, e a razão de o teto
// existir: cinco cultos numa semana movimentada enchem o disco muito antes de qualquer um deles
// completar 30 dias. Nenhum dos três aqui está fora do prazo.
func TestExpiraPorTetoDentroDoPrazo(t *testing.T) {
	c := cacheDeTeste(t)
	agora := c.agora()
	cultoNoCache(t, c, "cultoUsado01", 4, agora.Add(-5*dia)) // uso mais antigo → sai primeiro
	cultoNoCache(t, c, "cultoUsado02", 4, agora.Add(-3*dia))
	cultoNoCache(t, c, "cultoUsado03", 4, agora.Add(-1*dia))

	// 12 MB em cache, teto de 9 MB: cabe removendo UM (o de uso mais antigo).
	res, err := c.Expirar(OpcoesExpiracao{Teto: 9 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 1 {
		t.Fatalf("esperava 1 culto expirado pelo teto, veio %d: %+v", len(res.Cultos), res.Cultos)
	}
	if res.Cultos[0].VideoID != "cultoUsado01" {
		t.Errorf("expirou %q; o teto tem de comer o de ÚLTIMO USO mais antigo", res.Cultos[0].VideoID)
	}
	if res.Cultos[0].Motivo != MotivoTeto {
		t.Errorf("motivo = %q, quero %q (nenhum deles estourou o prazo)", res.Cultos[0].Motivo, MotivoTeto)
	}
	if res.AcimaDoTeto {
		t.Error("relatou que continua acima do teto, mas removeu o suficiente")
	}
	// Para de remover assim que cabe: os dois mais novos ficam inteiros.
	for _, id := range []string{"cultoUsado02", "cultoUsado03"} {
		if !temArquivo(t, c, id, NomeVideo) {
			t.Errorf("%s foi apagado à toa: o cache já cabia no teto", id)
		}
	}
}

// TestTetoNaoRemoveNadaSeJaCabe: a régua do teto não é "sempre apaga o mais antigo".
func TestTetoNaoRemoveNadaSeJaCabe(t *testing.T) {
	c := cacheDeTeste(t)
	cultoNoCache(t, c, "cultoUsado01", 4, c.agora().Add(-10*dia))

	res, err := c.Expirar(OpcoesExpiracao{Teto: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 0 {
		t.Errorf("expirou %d culto(s) com o cache dentro do prazo e dentro do teto", len(res.Cultos))
	}
	if len(res.Retidos) != 1 {
		t.Errorf("retidos = %v, quero o culto listado como retido", res.Retidos)
	}
}

// TestUltimoUsoProtegeOCultoReprocessado é o motivo de a idade ser por usado_em e não por
// baixado_em: um culto baixado há 60 dias e reaproveitado ontem é o vídeo MAIS útil do cache, e
// FIFO puro apagaria justamente ele.
func TestUltimoUsoProtegeOCultoReprocessado(t *testing.T) {
	c := cacheDeTeste(t)
	agora := c.agora()
	cultoNoCache(t, c, "cultoAntigo1", 1, agora.Add(-2*dia)) // baixado há 60 dias (abaixo), usado ontem
	dir, _ := c.DirVideo("cultoAntigo1")
	idx, err := c.LerIndice("cultoAntigo1")
	if err != nil {
		t.Fatal(err)
	}
	idx.BaixadoEm = agora.Add(-60 * dia)
	if err := c.gravarIndice(dir, idx); err != nil {
		t.Fatal(err)
	}

	res, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 0 {
		t.Fatalf("expirou um culto com download de 60 dias e USO de 2 dias: a idade está saindo "+
			"do baixado_em (%+v)", res.Cultos)
	}
}

// TestVideoEmUsoEIntocavel: a invariante caríssima. Apagar o vídeo de um pedido que está
// renderizando estraga um trabalho em andamento com um erro do ffmpeg que não fala do problema.
//
// O culto aqui estoura o prazo E o teto ao mesmo tempo — as duas réguas têm de ceder ao "está
// em uso", não só uma.
func TestVideoEmUsoEIntocavel(t *testing.T) {
	c := cacheDeTeste(t)
	cultoNoCache(t, c, "cultoEmUso01", 4, c.agora().Add(-90*dia))

	res, err := c.Expirar(OpcoesExpiracao{Teto: 1, Intocaveis: []string{"cultoEmUso01"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 0 {
		t.Fatalf("expirou o vídeo de um pedido em curso: %+v", res.Cultos)
	}
	if !temArquivo(t, c, "cultoEmUso01", NomeVideo) {
		t.Fatal("o vídeo em uso foi apagado")
	}
	if len(res.EmUso) != 1 {
		t.Errorf("em uso = %v, quero o culto listado (o operador precisa saber por que sobrou)", res.EmUso)
	}
	// E fica dito que o teto não foi atingido, em vez de o resumo dizer que arrumou tudo.
	if !res.AcimaDoTeto {
		t.Error("não relatou que o cache continua acima do teto por causa do vídeo em uso")
	}
	if !strings.Contains(res.Resumo(), "em uso") {
		t.Errorf("o resumo não menciona o intocável: %q", res.Resumo())
	}
}

// TestExpiracaoPreservaLegendaETranscricao: expirar é apagar o PESO, não a pasta.
//
// A lista de quem pode sair é a de retencao.PodeRemover — a MESMA da limpeza por pedido. Este
// teste é o que trava a decisão: apagar a legenda aqui recriaria, um nível acima, a contradição
// que a spec-06 tinha ("baixa de novo", escrito antes de o cache existir).
func TestExpiracaoPreservaLegendaETranscricao(t *testing.T) {
	c := cacheDeTeste(t)
	cultoNoCache(t, c, "cultoVelho1", 1, c.agora().Add(-40*dia))
	if err := c.GarantirTranscricaoIntegra("cultoVelho1"); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Expirar(OpcoesExpiracao{}); err != nil {
		t.Fatal(err)
	}
	for _, nome := range []string{NomeVideo, NomeInfo} {
		if temArquivo(t, c, "cultoVelho1", nome) {
			t.Errorf("%s deveria ter sido apagado (é material bruto regenerável)", nome)
		}
	}
	for _, nome := range []string{NomeLegenda, NomeTransc, NomeIndice} {
		if !temArquivo(t, c, "cultoVelho1", nome) {
			t.Errorf("%s foi apagado: são 400 KB que custam 3 s e uma requisição ao YouTube, e a "+
				"legenda saiu dos removíveis de propósito", nome)
		}
	}
	// E, sem o vídeo, o cache responde corretamente que só falta o vídeo — quem pedir este culto
	// de novo baixa 570 MB e não toca na legenda.
	if c.TemVideo("cultoVelho1") {
		t.Error("TemVideo continua dizendo que sim depois da expiração")
	}
	if !c.TemLegenda("cultoVelho1") {
		t.Error("TemLegenda diz que não: o próximo pedido baixaria a legenda à toa")
	}
}

// TestExpirarDeNovoNaoRelista: idempotência sem carimbo novo. Um culto já expirado tem zero
// arquivos removíveis, e é isso que o exclui da segunda passagem — sem um campo "expirado_em"
// para alguém esquecer de gravar.
func TestExpirarDeNovoNaoRelista(t *testing.T) {
	c := cacheDeTeste(t)
	cultoNoCache(t, c, "cultoVelho1", 1, c.agora().Add(-40*dia))

	primeira, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatal(err)
	}
	segunda, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatal(err)
	}
	if len(primeira.Cultos) != 1 {
		t.Fatalf("a primeira passagem deveria expirar 1 culto, expirou %d", len(primeira.Cultos))
	}
	if len(segunda.Cultos) != 0 || segunda.BytesLiberados != 0 {
		t.Errorf("a segunda passagem relistou o que já tinha expirado: %+v", segunda.Cultos)
	}
}

// TestDryRunNaoApaga: o operador precisa poder olhar antes.
func TestDryRunNaoApaga(t *testing.T) {
	c := cacheDeTeste(t)
	cultoNoCache(t, c, "cultoVelho1", 2, c.agora().Add(-40*dia))

	res, err := c.Expirar(OpcoesExpiracao{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 1 || res.BytesLiberados < 2<<20 {
		t.Fatalf("o dry-run deveria RELATAR a remoção com os bytes: %+v", res)
	}
	if !temArquivo(t, c, "cultoVelho1", NomeVideo) {
		t.Fatal("o dry-run apagou o vídeo")
	}
	if !strings.Contains(res.Resumo(), "dry-run") {
		t.Errorf("o resumo não avisa que nada foi apagado: %q", res.Resumo())
	}
}

// TestExpirarCacheInexistenteNaoEErro: servidor novo, antes do primeiro download.
func TestExpirarCacheInexistenteNaoEErro(t *testing.T) {
	c := Novo(filepath.Join(t.TempDir(), "nunca-existiu"))
	res, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatalf("cache inexistente virou erro: %v", err)
	}
	if len(res.Cultos) != 0 {
		t.Errorf("expirou algo num cache vazio: %+v", res.Cultos)
	}
}

// TestCultoSemIndiceUsaOMtimeENaoIdadeZero: cache preenchido à mão (ou migração antiga) não tem
// video.json. Tratar "sem índice" como idade zero deixaria esse culto no disco para sempre —
// exatamente o vídeo que ninguém registrou e ninguém vai reprocessar.
func TestCultoSemIndiceUsaOMtimeENaoIdadeZero(t *testing.T) {
	c := cacheDeTeste(t)
	dir := comLegenda(t, c, "cultoSemIdx1")
	alvo := filepath.Join(dir, NomeVideo)
	if err := os.WriteFile(alvo, make([]byte, 1<<20), 0644); err != nil {
		t.Fatal(err)
	}
	velho := c.agora().Add(-100 * dia)
	for _, nome := range []string{NomeVideo, NomeLegenda} {
		if err := os.Chtimes(filepath.Join(dir, nome), velho, velho); err != nil {
			t.Fatal(err)
		}
	}

	res, err := c.Expirar(OpcoesExpiracao{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cultos) != 1 {
		t.Fatalf("culto sem video.json e com 100 dias de mtime não expirou: %+v", res)
	}
}
