package servidor

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Este arquivo existe por causa de um bug real: um recorte de bloco durante o redesenho do
// painel de ajuste engoliu playPause, doInicio, emendaInicio e emendaFim. O `node --check`
// passou (sintaxe válida), os testes de contrato passaram (as strings que eles procuravam
// continuavam lá), e a tela quebrou no primeiro clique com "playPause is not defined".
//
// A lição: verificar PRESENÇA DE STRING não verifica INTEGRIDADE DE REFERÊNCIA. Um handler
// que aponta para função inexistente é sintaticamente perfeito e completamente quebrado.
// Estes testes fazem a checagem que faltava, sem depender de navegador nem de node.

var (
	reDeclFunc = regexp.MustCompile(`function\s+([A-Za-z_$][\w$]*)\s*\(`)
	reDeclVar  = regexp.MustCompile(`(?:var|let|const)\s+([A-Za-z_$][\w$]*)`)
	reChamada  = regexp.MustCompile(`(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*\(`)
	reHandler  = regexp.MustCompile(`addEventListener\(\s*'[^']+'\s*,\s*([A-Za-z_$][\w$]*)\s*\)`)
	// ligar('id', nomeDaFuncao) — o atalho da tela para addEventListener('click', ...).
	reLigarFn = regexp.MustCompile(`ligar\('[^']+',\s*([A-Za-z_$][\w$]*)\s*\)`)
	// os ids que ligar() vai buscar: precisam existir no HTML tanto quanto os do
	// getElementById literal, senão o controle simplesmente não liga.
	reLigarID   = regexp.MustCompile(`ligar\('([^']+)'`)
	reComentari = regexp.MustCompile(`(?m)//.*$`)
	reBloco     = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// Literais de string precisam sair antes da análise: um texto como "começo da fala (" é
	// lido pela regex de chamada como uma chamada a `fala`.
	reStr1 = regexp.MustCompile(`'(?:[^'\\\n]|\\.)*'`)
	reStr2 = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`)
)

// globaisPermitidos são os nomes que o script pode chamar sem declarar: APIs do navegador, do
// YouTube e do HTMX. Manter explícito é o que dá valor ao teste — um nome novo aqui é uma
// decisão consciente, não um acidente.
var globaisPermitidos = map[string]bool{
	// navegador
	"setTimeout": true, "clearTimeout": true, "setInterval": true, "clearInterval": true,
	"fetch": true, "parseInt": true, "parseFloat": true, "isNaN": true, "encodeURIComponent": true,
	"JSON": true, "Math": true, "String": true, "Number": true, "Array": true, "Object": true,
	"alert": true, "confirm": true, "console": true,
	// terceiros / plataforma
	"YT": true, "htmx": true,
	// palavras-chave que a regex de chamada pode capturar por engano
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"function": true, "typeof": true, "new": true, "else": true, "do": true,
}

// jsDaPagina extrai o <script> inline da página SEM comentários mas COM os literais de
// string — é o que os testes de handler e de id precisam ler.
func jsDaPagina(t *testing.T) string {
	t.Helper()
	pagina := htmlDaPagina(t)
	ini := strings.Index(pagina, "<script>")
	if ini < 0 {
		t.Fatal("a página não tem <script> inline")
	}
	fim := strings.Index(pagina[ini:], "</script>")
	if fim < 0 {
		t.Fatal("<script> sem fechamento")
	}
	js := pagina[ini+len("<script>") : ini+fim]
	js = reBloco.ReplaceAllString(js, "")
	return reComentari.ReplaceAllString(js, "")
}

// jsSemLiterais tira também as strings, para a análise de CHAMADAS: um texto como
// "começo da fala (" seria lido pela regex como uma chamada a `fala`.
func jsSemLiterais(t *testing.T) string {
	js := reStr1.ReplaceAllString(jsDaPagina(t), `''`)
	return reStr2.ReplaceAllString(js, `""`)
}

// declarados devolve tudo que o script define: funções, vars, lets, consts e parâmetros.
func declarados(js string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reDeclFunc.FindAllStringSubmatch(js, -1) {
		out[m[1]] = true
	}
	for _, m := range reDeclVar.FindAllStringSubmatch(js, -1) {
		out[m[1]] = true
	}
	// Parâmetros de função (incluindo arrow e function(x, y)).
	reParams := regexp.MustCompile(`function\s*[\w$]*\s*\(([^)]*)\)`)
	for _, m := range reParams.FindAllStringSubmatch(js, -1) {
		for _, p := range strings.Split(m[1], ",") {
			if p = strings.TrimSpace(p); p != "" {
				out[p] = true
			}
		}
	}
	return out
}

// TestHandlersApontamParaFuncoesExistentes é o teste que teria pegado o bug: um
// addEventListener que referencia função removida.
func TestHandlersApontamParaFuncoesExistentes(t *testing.T) {
	js := jsDaPagina(t)
	decl := declarados(js)

	// Os dois padrões usados na tela: addEventListener direto e o atalho ligar().
	var refs []string
	for _, m := range reHandler.FindAllStringSubmatch(js, -1) {
		refs = append(refs, m[1])
	}
	for _, m := range reLigarFn.FindAllStringSubmatch(js, -1) {
		refs = append(refs, m[1])
	}
	if len(refs) < 5 {
		t.Fatalf("só %d handlers por referência direta encontrados — a regex ou a tela mudou", len(refs))
	}
	for _, nome := range refs {
		if !decl[nome] {
			t.Errorf("um handler aponta para %q, que não existe no script — a tela quebra no clique", nome)
		}
	}
}

// TestNenhumaChamadaAFuncaoInexistente é a versão ampla: toda função chamada no script tem de
// estar declarada ou constar na lista de globais permitidos. Pega remoção acidental em
// qualquer ponto, não só nos handlers.
func TestNenhumaChamadaAFuncaoInexistente(t *testing.T) {
	js := jsSemLiterais(t)
	decl := declarados(js)

	var faltando []string
	vistos := map[string]bool{}
	for _, m := range reChamada.FindAllStringSubmatch(js, -1) {
		nome := m[1]
		if decl[nome] || globaisPermitidos[nome] || vistos[nome] {
			continue
		}
		vistos[nome] = true
		faltando = append(faltando, nome)
	}
	sort.Strings(faltando)
	if len(faltando) > 0 {
		t.Errorf("chamadas a nomes não declarados nem permitidos: %v\n"+
			"Se for uma API do navegador legítima, some à lista globaisPermitidos — "+
			"conscientemente, que é o valor deste teste.", faltando)
	}
}

// TestFuncoesEssenciaisDaTelaExistem lista por nome o que a tela não pode perder. É
// redundante com os testes acima de propósito: se um recorte remover a função E o handler
// juntos, os testes de referência ficam satisfeitos e a funcionalidade desaparece calada.
func TestFuncoesEssenciaisDaTelaExistem(t *testing.T) {
	js := jsDaPagina(t)
	decl := declarados(js)
	essenciais := []string{
		// revisão
		"iniciarRevisao", "criarPlayer", "tocarIntervalo", "trechoAtual", "desenhar",
		"desenharTrilha", "irPara", "decidir", "confirmar", "ligarControles",
		// player
		"playPause", "doInicio", "ouvirInicio", "ouvirFim", "meiaVelocidade", "limiteFim",
		// ajuste (spec-05 v2)
		"efetivo", "efetivoAtual", "origMs", "rotulo", "moverPara", "empurrar",
		"clicarFrase", "fimDaFraseSeguinte", "restaurar", "pedirRecalculo",
		"desenharFrases", "desenharAjuste", "garantirVizinhanca", "escapar",
		"ligar", "avisarToque", "rolarAteSelecionadas",
	}
	for _, f := range essenciais {
		if !decl[f] {
			t.Errorf("função essencial ausente: %s", f)
		}
	}
}

// TestGetElementByIdSoAponaParaIdsQueExistem: um id renomeado no HTML e não no JS produz
// null.addEventListener — mesma classe de falha, outro lado. Confere contra a página inteira
// (que traz o fragmento de revisão nos templates).
func TestGetElementByIdSoApontaParaIdsQueExistem(t *testing.T) {
	pagina := htmlDaPagina(t)
	revisao := htmlDaRevisao(t)
	tudo := pagina + revisao

	reGet := regexp.MustCompile(`getElementById\('([^']+)'\)`)
	js := jsDaPagina(t)
	var ids []string
	for _, m := range reGet.FindAllStringSubmatch(js, -1) {
		ids = append(ids, m[1])
	}
	// ligar('id', fn) resolve o id por dentro; sem incluí-lo aqui, trocar
	// getElementById por ligar() abriria um buraco silencioso nesta verificação.
	for _, m := range reLigarID.FindAllStringSubmatch(js, -1) {
		ids = append(ids, m[1])
	}
	vistos := map[string]bool{}
	for _, id := range ids {
		if vistos[id] {
			continue
		}
		vistos[id] = true
		if !strings.Contains(tudo, `id="`+id+`"`) {
			t.Errorf("o JS busca o id %q, que não existe em nenhum template — o controle não liga", id)
		}
	}
	if len(vistos) < 15 {
		t.Fatalf("só %d ids consultados — a extração do JS provavelmente falhou", len(vistos))
	}
}
