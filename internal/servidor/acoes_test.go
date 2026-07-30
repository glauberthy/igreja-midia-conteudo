package servidor

import (
	"encoding/csv"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// O histórico de ações existe para separar DUAS causas do "o áudio não bate com o trecho":
// legenda adiantada (Rota D) ou encaixe/clamp nosso (bug corrigível). O que separa é o PAR
// pedido/aplicado — e é isso que estes testes protegem.

// O acoes.csv é escrito DENTRO do handler /aprovar (síncrono, antes de a fase pesada começar),
// então nenhum teste aqui precisa esperar estado — o que se afirma é sobre o arquivo, e ele já
// está lá quando o POST responde.

// aprovarComAcoes envia aprovação + histórico pelo mesmo caminho que o cliente usa (formulário
// com o campo "acoes" em JSON).
func aprovarComAcoes(t *testing.T, s *Servidor, id, acoesJSON string, indices ...string) *httptest.ResponseRecorder {
	t.Helper()
	partes := []string{}
	for _, i := range indices {
		partes = append(partes, "aprovados="+i)
	}
	partes = append(partes, "acoes="+url.QueryEscape(acoesJSON))
	req := httptest.NewRequest("POST", "/pedidos/"+id+"/aprovar", strings.NewReader(strings.Join(partes, "&")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func lerAcoesCSV(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("acoes.csv não foi escrito: %v", err)
	}
	reg, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatalf("acoes.csv não é CSV válido: %v\n%s", err, b)
	}
	return reg
}

func campoAcao(t *testing.T, reg [][]string, linha int, nome string) string {
	t.Helper()
	for i, c := range reg[0] {
		if c == nome {
			return reg[linha][i]
		}
	}
	t.Fatalf("coluna %q não existe no acoes.csv (tem %v)", nome, reg[0])
	return ""
}

// TestHistoricoRegistraPedidoEAplicado é o teste do PAR. Sem as duas colunas o log não distingue
// nada — e é justamente o que o cortes.csv não tem.
func TestHistoricoRegistraPedidoEAplicado(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())

	// Duas ações: um clique de frase que o encaixe MOVEU (pediu 40000, aplicou 41000) e um
	// empurrão fino que o sistema obedeceu (pediu 80250, aplicou 80250).
	acoes := `[
	  {"indice":0,"seq":1,"tipo":"frase-inicio","pedido_ms":40000,"aplicado_ms":41000,
	   "aplicado_ok":true,"inicio_ms":41000,"fim_ms":80000,"duracao_ms":39000,
	   "frase":"porque Deus sempre governou a historia"},
	  {"indice":0,"seq":2,"tipo":"fino-fim","pedido_ms":80250,"aplicado_ms":80250,
	   "aplicado_ok":true,"inicio_ms":41000,"fim_ms":80250,"duracao_ms":39250}
	]`
	if rec := aprovarComAcoes(t, s, "teste-1", acoes, "0"); rec.Code != 200 {
		t.Fatalf("aprovar devolveu %d: %s", rec.Code, rec.Body.String())
	}
	reg := lerAcoesCSV(t, s.acoesPath)
	if len(reg) != 3 { // cabeçalho + 2 ações
		t.Fatalf("esperava cabeçalho + 2 ações, veio %d linha(s): %v", len(reg), reg)
	}

	// Ação 1: o sistema NÃO aplicou o que o operador pediu — é o caso "bug nosso", e o delta
	// tem de estar lá, com sinal.
	if got := campoAcao(t, reg, 1, "pedido_ms"); got != "40000" {
		t.Errorf("pedido_ms = %q, quero 40000", got)
	}
	if got := campoAcao(t, reg, 1, "aplicado_ms"); got != "41000" {
		t.Errorf("aplicado_ms = %q, quero 41000", got)
	}
	if got := campoAcao(t, reg, 1, "delta_ms"); got != "1000" {
		t.Errorf("delta_ms = %q, quero 1000 — é a coluna que acusa o encaixe", got)
	}
	if got := campoAcao(t, reg, 1, "frase"); !strings.Contains(got, "governou") {
		t.Errorf("frase clicada não foi registrada: %q", got)
	}

	// Ação 2: o sistema obedeceu — delta 0 é o que aponta para a legenda, não para nós.
	if got := campoAcao(t, reg, 2, "delta_ms"); got != "0" {
		t.Errorf("delta_ms = %q, quero 0 (o sistema aplicou o pedido)", got)
	}
	if got := campoAcao(t, reg, 2, "tipo"); got != "fino-fim" {
		t.Errorf("tipo = %q, quero fino-fim", got)
	}
	if got := campoAcao(t, reg, 2, "decisao"); got != "aprovado" {
		t.Errorf("decisao = %q, quero aprovado", got)
	}
}

// TestHistoricoGuardaOQueOOperadorOuviu: o terceiro dado. pedido/aplicado provam fidelidade do
// sistema; só o ouvido diz DE QUANTO é o desvio real, e é esse número que decide entre um
// deslocamento fixo e a Rota D.
func TestHistoricoGuardaOQueOOperadorOuviu(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	acoes := `[{"indice":0,"seq":1,"tipo":"ouvido","inicio_ms":36000,"fim_ms":80000,
	            "duracao_ms":44000,"ouvido":"faltou ~1s no fim, cortou \"toca\""}]`
	if rec := aprovarComAcoes(t, s, "teste-1", acoes, "0"); rec.Code != 200 {
		t.Fatalf("aprovar devolveu %d: %s", rec.Code, rec.Body.String())
	}
	reg := lerAcoesCSV(t, s.acoesPath)
	got := campoAcao(t, reg, 1, "ouvido")
	if !strings.Contains(got, "faltou ~1s") {
		t.Errorf("a nota do operador não sobreviveu: %q", got)
	}
	// Vírgula e aspas na frase livre não podem partir a linha — o campo é digitado por humano.
	if len(reg[1]) != len(reg[0]) {
		t.Errorf("a linha tem %d campos e o cabeçalho %d: o texto livre quebrou o CSV",
			len(reg[1]), len(reg[0]))
	}
}

// TestHistoricoMarcaAcaoSubstituida: o debounce junta uma rajada de empurrões, então só a última
// ação tem resposta do servidor. Inventar um aplicado para as anteriores seria pior que deixar em
// branco — o log passaria a afirmar o que não sabe.
func TestHistoricoMarcaAcaoSubstituida(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	acoes := `[
	  {"indice":0,"seq":1,"tipo":"fino-fim","pedido_ms":80250,"aplicado_ok":false,
	   "inicio_ms":36000,"fim_ms":80250,"duracao_ms":44250},
	  {"indice":0,"seq":2,"tipo":"fino-fim","pedido_ms":80500,"aplicado_ms":80500,
	   "aplicado_ok":true,"inicio_ms":36000,"fim_ms":80500,"duracao_ms":44500}
	]`
	if rec := aprovarComAcoes(t, s, "teste-1", acoes, "0"); rec.Code != 200 {
		t.Fatalf("aprovar devolveu %d", rec.Code)
	}
	reg := lerAcoesCSV(t, s.acoesPath)
	if got := campoAcao(t, reg, 1, "aplicado_ms"); got != "" {
		t.Errorf("aplicado_ms da ação substituída = %q, quero vazio", got)
	}
	if got := campoAcao(t, reg, 1, "delta_ms"); got != "" {
		t.Errorf("delta_ms da ação substituída = %q, quero vazio (não há aplicado para comparar)", got)
	}
	if got := campoAcao(t, reg, 2, "aplicado_ms"); got != "80500" {
		t.Errorf("a última ação da rajada perdeu o aplicado: %q", got)
	}
}

// TestHistoricoRegistraTrechoReprovado: um trecho que o operador mexeu e desistiu é evidência
// igual. A coluna decisao separa o que foi entregue do que foi descartado.
func TestHistoricoRegistraTrechoReprovado(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	acoes := `[
	  {"indice":0,"seq":1,"tipo":"fino-fim","pedido_ms":80250,"aplicado_ms":80250,
	   "aplicado_ok":true,"inicio_ms":36000,"fim_ms":80250,"duracao_ms":44250},
	  {"indice":1,"seq":1,"tipo":"fino-inicio","pedido_ms":90000,"aplicado_ms":90000,
	   "aplicado_ok":true,"inicio_ms":90000,"fim_ms":120000,"duracao_ms":30000}
	]`
	if rec := aprovarComAcoes(t, s, "teste-1", acoes, "0"); rec.Code != 200 {
		t.Fatalf("aprovar devolveu %d", rec.Code)
	}
	reg := lerAcoesCSV(t, s.acoesPath)
	if len(reg) != 3 {
		t.Fatalf("esperava as ações dos DOIS trechos, veio %d linha(s)", len(reg))
	}
	if got := campoAcao(t, reg, 1, "decisao"); got != "aprovado" {
		t.Errorf("trecho 0: decisao = %q, quero aprovado", got)
	}
	if got := campoAcao(t, reg, 2, "decisao"); got != "reprovado" {
		t.Errorf("trecho 1: decisao = %q, quero reprovado", got)
	}
}

// TestHistoricoIlegivelNaoTravaAAprovacao: evidência não pode custar o produto. Se o JSON do
// histórico vier torto (JS antigo, cliente estranho), o pedido segue.
func TestHistoricoIlegivelNaoTravaAAprovacao(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	if rec := aprovarComAcoes(t, s, "teste-1", "{isto não é json}", "0"); rec.Code != 200 {
		t.Fatalf("histórico torto derrubou a aprovação: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(s.acoesPath); err == nil {
		t.Error("gravou acoes.csv a partir de JSON inválido")
	}
}

// TestCabecalhoAcoesSaiDaMesmaListaQueAsLinhas: o tempos.csv quebrou DUAS vezes por ter uma lista
// de nomes e outra de valores. Aqui nome e valor moram na mesma entrada, e este teste é o que
// impede a regressão — conta os campos com encoding/csv, porque contar vírgula em linha com campo
// entre aspas mede a coisa errada (erro real cometido na migração do tempos.csv).
func TestCabecalhoAcoesSaiDaMesmaListaQueAsLinhas(t *testing.T) {
	l := linhaAcao{
		quando: "2026-07-30T13:00:00Z", pedido: "web-1", decisao: "aprovado",
		a: Acao{Indice: 0, Seq: 1, Tipo: "frase-inicio", PedidoMs: 40000, AplicadoMs: 41000,
			AplicadoOk: true, InicioMs: 41000, FimMs: 80000, DuracaoMs: 39000,
			Frase: "uma frase com vírgula, aspas \" e tudo", Ouvido: "faltou ~1s"},
	}
	reg, err := csv.NewReader(strings.NewReader(l.csv())).ReadAll()
	if err != nil || len(reg) != 1 {
		t.Fatalf("a linha não é um CSV de um registro: %v (%q)", err, l.csv())
	}
	nCab := len(strings.Split(strings.TrimRight(cabecalhoAcoes, "\n"), ","))
	if len(reg[0]) != nCab {
		t.Errorf("linha com %d campos, cabeçalho com %d", len(reg[0]), nCab)
	}
}

// TestTelaTrazOHistorico liga o instrumento à tela: sem estes ids o log existiria no servidor e
// o operador não teria como ler nem trazer a evidência — que é o ponto dele.
//
// Verifica ids, não aparência. O julgamento visual é do dono (regra 7 do CLAUDE.md).
func TestTelaTrazOHistorico(t *testing.T) {
	corpo := htmlDaRevisao(t)
	for _, quer := range []string{
		`id="historico"`,   // o <details> colapsável
		`id="hist-n"`,      // a contagem no summary
		`id="hist-linhas"`, // uma linha por ação
		`id="hist-ouvido"`, // o TERCEIRO dado: o que ele percebeu
		`id="hist-anotar"`,
		`id="hist-copiar"`, // levar a evidência para fora
	} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("a tela de revisão não trouxe %q", quer)
		}
	}
	// O campo do ouvido precisa de placeholder com EXEMPLO: "o que você ouviu" sem exemplo
	// recebe respostas inúteis, e o campo opcional que dá trabalho não é preenchido.
	if !strings.Contains(corpo, "faltou ~1s no fim") {
		t.Error("o campo do ouvido não sugere o formato da anotação")
	}
}
