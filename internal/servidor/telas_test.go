package servidor

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
)

// O requisito central da Parte 1 da spec-05 v3: NAVEGAÇÃO NO CLIENTE, AÇÕES NO SERVIDOR.
//
// Trocar de tela não pode gerar requisição — o operador precisa ir e voltar durante a revisão
// sem esperar servidor. Isso é verificável sem navegador, olhando o que a página serve, e é o
// que estes testes fazem. O que eles NÃO provam está dito no fim do arquivo.

// TestAsQuatroTelasEstaoNoDOM: as quatro existem desde o primeiro carregamento. Se uma delas
// voltar a ser montada pelo servidor, a navegação para ela volta a custar requisição.
func TestAsQuatroTelasEstaoNoDOM(t *testing.T) {
	pagina := htmlDaPagina(t)
	for _, id := range []string{"tela-dados", "tela-processando", "tela-revisao", "tela-resultado"} {
		if !strings.Contains(pagina, `id="`+id+`"`) {
			t.Errorf("a página não traz %q: a tela teria de ser buscada no servidor", id)
		}
	}
	// Três começam escondidas e a primeira aparece: é o estado inicial correto.
	if !strings.Contains(pagina, `id="tela-processando" hidden`) ||
		!strings.Contains(pagina, `id="tela-revisao" hidden`) ||
		!strings.Contains(pagina, `id="tela-resultado" hidden`) {
		t.Error("as telas 2-4 deveriam vir com `hidden` (só a de dados aparece no início)")
	}
}

// TestNavegarNaoGeraRequisicao é o teste que guarda o requisito.
//
// Verifica no HTML: nenhum elemento do indicador de etapas carrega atributo hx-*, e o JS de
// navegação não faz requisição. Não é o mesmo que rodar num navegador — mas pega a regressão
// realista, que é alguém "resolver" a navegação pendurando um hx-get num botão de etapa.
func TestNavegarNaoGeraRequisicao(t *testing.T) {
	pagina := htmlDaPagina(t)

	// 1) O bloco do indicador de etapas, isolado, não pode ter atributo hx-*.
	ini := strings.Index(pagina, `id="etapas"`)
	if ini < 0 {
		t.Fatal("a página não traz o indicador de etapas (#etapas)")
	}
	fim := strings.Index(pagina[ini:], "</nav>")
	if fim < 0 {
		t.Fatal("o indicador de etapas não fecha em </nav>")
	}
	bloco := pagina[ini : ini+fim]
	if strings.Contains(bloco, "hx-") {
		t.Errorf("o indicador de etapas tem atributo HTMX — trocar de tela passaria a fazer "+
			"requisição, que é exatamente o que a Parte 1 elimina:\n%s", bloco)
	}
	// Contraprova: os botões existem mesmo (senão o teste acima passa por vacuidade).
	if n := strings.Count(bloco, "data-ir="); n != 4 {
		t.Errorf("esperava 4 botões de etapa com data-ir, achei %d", n)
	}

	// 2) A função que troca de tela não pode conter chamada de rede.
	js := jsDaPagina(t)
	corpo := corpoDaFuncao(t, js, "mostrarTela")
	for _, proibido := range []string{"fetch(", "htmx.ajax", "XMLHttpRequest", "location.href"} {
		if strings.Contains(corpo, proibido) {
			t.Errorf("mostrarTela contém %q: navegar tem de ser local\n%s", proibido, corpo)
		}
	}
	if !strings.Contains(corpo, "hidden") {
		t.Errorf("mostrarTela não mexe em `hidden` — é assim que a troca é local:\n%s", corpo)
	}
}

// TestOIndicadorNaoDeixaPularEtapa: quem libera etapa é o servidor (pelo estado do pedido),
// não o clique. Sem isso o operador cairia numa revisão sem candidatos.
func TestOIndicadorNaoDeixaPularEtapa(t *testing.T) {
	js := jsDaPagina(t)
	corpo := corpoDaFuncao(t, js, "mostrarTela")
	if !strings.Contains(corpo, "APP.alcancadas[nome]") {
		t.Errorf("mostrarTela não consulta APP.alcancadas: um clique poderia abrir etapa que o "+
			"servidor ainda não liberou\n%s", corpo)
	}
	// E o desenho do indicador desabilita o botão do que não foi alcançado.
	desenho := corpoDaFuncao(t, js, "desenharEtapas")
	if !strings.Contains(desenho, "b.disabled") {
		t.Errorf("desenharEtapas não desabilita botão de etapa não alcançada:\n%s", desenho)
	}
}

// TestPedidoAtualSemPedidoDevolve204: primeira visita não é erro. Com 204 o HTMX não troca
// nada e a página fica na tela de dados — que é o certo.
func TestPedidoAtualSemPedidoDevolve204(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{transc: "x"}, &selecionadorFake{})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pedido-atual", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("código %d, quero 204: 'não há pedido' é resposta normal, não erro", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 com corpo (%d bytes): o HTMX trocaria o DOM por nada", rec.Body.Len())
	}
}

// TestPedidoAtualReidrataOEstado é a recuperação do F5: com um pedido em curso no servidor,
// abrir a página tem de trazer o estado — senão recarregar no meio de uma seleção de 30 s
// jogaria o operador de volta ao formulário, com o pedido rodando invisível.
func TestPedidoAtualReidrataOEstado(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)
	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pedido-atual", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("código %d, quero 200", rec.Code)
	}
	corpo := rec.Body.String()
	for _, quer := range []string{`id="estado-json"`, `id="dados-trechos"`, id, "aguardando-aprovacao"} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("a reidratação não trouxe %q:\n%s", quer, corpo)
		}
	}
	// E é UMA requisição no load, não navegação: o gatilho é `load`.
	if !strings.Contains(htmlDaPagina(t), `hx-get="/pedido-atual" hx-trigger="load"`) {
		t.Error("a página não busca o estado no load — o F5 perderia o pedido em curso")
	}
}

// TestAvisoAntesDeSairComDecisaoNaoEnviada: a decisão registrada é avisar e aceitar a perda.
// O teste guarda a existência do aviso; que o navegador o exiba não é verificável aqui.
func TestAvisoAntesDeSairComDecisaoNaoEnviada(t *testing.T) {
	js := jsDaPagina(t)
	if !strings.Contains(js, "beforeunload") {
		t.Error("sem aviso de saída: um F5 no meio da revisão perderia as decisões em silêncio")
	}
	if !strings.Contains(js, "APP.sujo") {
		t.Error("o aviso não consulta APP.sujo — avisaria sempre, e aviso que aparece sem motivo " +
			"ensina o operador a ignorar")
	}
}

// TestTelaDeProcessamentoMostraAsDuasEtapas: legenda e seleção são UMA etapa na navegação
// (baixar legenda leva 3 s e não merece tela própria), mas DUAS linhas — a seleção é a que
// custa ~30 s e é ela que precisa de sinal de vida.
func TestTelaDeProcessamentoMostraAsDuasEtapas(t *testing.T) {
	pagina := htmlDaPagina(t)
	for _, id := range []string{"et-legenda", "et-selecao", "et-video"} {
		if !strings.Contains(pagina, `id="`+id+`"`) {
			t.Errorf("a tela de processamento não traz a linha %q", id)
		}
	}
	if !strings.Contains(pagina, `id="et-selecao-quanto"`) {
		t.Error("a linha da seleção não tem onde mostrar o tempo decorrido: é a etapa de ~30 s, " +
			"e sem sinal de vida a tela parece travada")
	}
}

// corpoDaFuncao extrai o corpo de uma função do JS por contagem de chaves. É suficiente aqui
// (as funções da tela não têm string com chave desbalanceada) e evita depender de parser.
func corpoDaFuncao(t *testing.T, js, nome string) string {
	t.Helper()
	re := regexp.MustCompile(`function\s+` + regexp.QuoteMeta(nome) + `\s*\([^)]*\)\s*\{`)
	loc := re.FindStringIndex(js)
	if loc == nil {
		t.Fatalf("função %q não existe no JS da página", nome)
	}
	nivel, ini := 0, loc[1]-1
	for i := ini; i < len(js); i++ {
		switch js[i] {
		case '{':
			nivel++
		case '}':
			nivel--
			if nivel == 0 {
				return js[ini : i+1]
			}
		}
	}
	t.Fatalf("função %q não fecha", nome)
	return ""
}

// O QUE ESTES TESTES NÃO PROVAM, e é honesto dizer: que a tela FUNCIONA no navegador. Eles
// verificam o contrato do que é servido — telas presentes, ausência de hx-* na navegação, o
// JS mexendo em `hidden`, os endpoints respondendo. Clique, foco, layout e o comportamento do
// player continuam sendo verificação do operador na tela real.
