package harness

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// modeloFake registra a última chamada e devolve uma resposta canônica.
type modeloFake struct {
	resposta      string
	err           error
	ultimoSistema string
	ultimoUsuario string
	ultimoMax     int
}

func (m *modeloFake) Completar(ctx context.Context, sistema, usuario string, maxTokens int) (string, error) {
	m.ultimoSistema, m.ultimoUsuario, m.ultimoMax = sistema, usuario, maxTokens
	return m.resposta, m.err
}

const mapaJSON = `{
  "tema_central": "A graça suficiente de Deus",
  "estrutura": ["introdução", "o problema do pecado", "a suficiência da graça"],
  "blocos": [
    {"assunto": "abertura sobre a graça", "inicio_aprox": "00:00:01", "fim_aprox": "00:00:40"},
    {"assunto": "o pastor e o rebanho", "inicio_aprox": "00:00:41", "fim_aprox": "00:01:30"}
  ]
}`

const candidatosJSON = `{
  "candidatos": [
    {"bloco": "abertura sobre a graça", "frase_ancora": "a graça de deus é suficiente",
     "motivo": "abertura forte"},
    {"bloco": "o pastor e o rebanho", "frase_ancora": "o senhor é o meu pastor",
     "motivo": "imagem pastoral"}
  ]
}`

func TestFase1Mapa(t *testing.T) {
	fake := &modeloFake{resposta: mapaJSON}
	mapa, err := Fase1Mapa(context.Background(), fake, "PROMPT_MAPA", "[00:00:01] a graça de deus é suficiente")
	if err != nil {
		t.Fatalf("Fase1Mapa: %v", err)
	}
	if mapa.TemaCentral != "A graça suficiente de Deus" {
		t.Errorf("tema inesperado: %q", mapa.TemaCentral)
	}
	if len(mapa.Estrutura) != 3 || len(mapa.Blocos) != 2 {
		t.Errorf("estrutura/blocos inesperados: %+v", mapa)
	}
	if mapa.Blocos[0].InicioAprox != "00:00:01" {
		t.Errorf("borda do bloco inesperada: %+v", mapa.Blocos[0])
	}
	// A Fase 1 usa o prompt de sistema dado e a transcrição como user.
	if fake.ultimoSistema != "PROMPT_MAPA" {
		t.Errorf("prompt de sistema não repassado: %q", fake.ultimoSistema)
	}
	if fake.ultimoMax != maxTokensMapa {
		t.Errorf("max_tokens da fase 1 = %d, queria %d", fake.ultimoMax, maxTokensMapa)
	}
}

func TestFase1MapaJSONInvalido(t *testing.T) {
	capturaLog(t) // esgota as 3 tentativas; silencia os logs de retry
	fake := &modeloFake{resposta: "isto não é json"}
	if _, err := Fase1Mapa(context.Background(), fake, "P", "t"); err == nil {
		t.Error("esperava erro para JSON inválido")
	}
}

func TestFase1MapaVazio(t *testing.T) {
	capturaLog(t)
	// JSON válido mas sem blocos → erro (mapa inútil).
	fake := &modeloFake{resposta: `{"tema_central":"x","estrutura":[],"blocos":[]}`}
	if _, err := Fase1Mapa(context.Background(), fake, "P", "t"); err == nil {
		t.Error("esperava erro para mapa sem blocos")
	}
}

// Regressão: a Fase 1 falhava quando o modelo devolvia `estrutura` como lista de
// OBJETOS (visto em 2 de 4 execuções reais). O erro era
// "cannot unmarshal object into Go struct field Mapa.estrutura of type string".
// Agora o parse é tolerante e nunca quebra por causa deste campo descritivo.

func TestFase1EstruturaListaDeStrings(t *testing.T) {
	// O caso "correto" que o prompt pede — tem que continuar funcionando.
	j := `{"tema_central":"graça","estrutura":["introdução","o problema","a graça"],
	       "blocos":[{"assunto":"abertura","inicio_aprox":"00:00:01","fim_aprox":"00:00:40"}]}`
	mapa, err := Fase1Mapa(context.Background(), &modeloFake{resposta: j}, "P", "t")
	if err != nil {
		t.Fatalf("lista de strings deveria parsear: %v", err)
	}
	if len(mapa.Estrutura) != 3 || mapa.Estrutura[0] != "introdução" || mapa.Estrutura[2] != "a graça" {
		t.Errorf("estrutura inesperada: %#v", mapa.Estrutura)
	}
}

func TestFase1EstruturaListaDeObjetos(t *testing.T) {
	// O caso que QUEBRAVA: lista de objetos. Cada objeto vira texto legível.
	j := `{"tema_central":"graça","estrutura":[
	         {"titulo":"introdução","desc":"abre o tema"},
	         {"ponto":"o problema do pecado"},
	         {"a":"sem campo conhecido","b":"junta valores"}
	       ],
	       "blocos":[{"assunto":"abertura","inicio_aprox":"00:00:01","fim_aprox":"00:00:40"}]}`
	mapa, err := Fase1Mapa(context.Background(), &modeloFake{resposta: j}, "P", "t")
	if err != nil {
		t.Fatalf("lista de objetos NÃO deveria quebrar a Fase 1: %v", err)
	}
	if len(mapa.Estrutura) != 3 {
		t.Fatalf("esperava 3 itens de estrutura, veio %d: %#v", len(mapa.Estrutura), mapa.Estrutura)
	}
	if mapa.Estrutura[0] != "introdução" { // prefere o campo "titulo"
		t.Errorf("item 0 = %q; queria \"introdução\"", mapa.Estrutura[0])
	}
	if mapa.Estrutura[1] != "o problema do pecado" { // prefere o campo "ponto"
		t.Errorf("item 1 = %q; queria \"o problema do pecado\"", mapa.Estrutura[1])
	}
	if mapa.Estrutura[2] != "sem campo conhecido — junta valores" { // valores em ordem de chave (a, b)
		t.Errorf("item 2 = %q; queria os valores juntados em ordem de chave", mapa.Estrutura[2])
	}
}

func TestFase1EstruturaValorUnico(t *testing.T) {
	// Casos degenerados: estrutura como string única ou objeto único — não quebram.
	for _, j := range []string{
		`{"tema_central":"x","estrutura":"um esboço em texto corrido","blocos":[{"assunto":"a","inicio_aprox":"00:00:01","fim_aprox":"00:00:40"}]}`,
		`{"tema_central":"x","estrutura":{"titulo":"esboço único"},"blocos":[{"assunto":"a","inicio_aprox":"00:00:01","fim_aprox":"00:00:40"}]}`,
	} {
		mapa, err := Fase1Mapa(context.Background(), &modeloFake{resposta: j}, "P", "t")
		if err != nil {
			t.Fatalf("valor único de estrutura não deveria quebrar: %v (json=%s)", err, j)
		}
		if len(mapa.Estrutura) != 1 || mapa.Estrutura[0] == "" {
			t.Errorf("esperava 1 item não-vazio, veio %#v", mapa.Estrutura)
		}
	}
}

func TestFase2Candidatos(t *testing.T) {
	fake := &modeloFake{resposta: candidatosJSON}
	mapa := Mapa{TemaCentral: "A graça suficiente de Deus", Blocos: []BlocoEnsino{{Assunto: "abertura"}}}

	cands, err := Fase2Candidatos(context.Background(), fake, "PROMPT_CAND", mapa, "[00:00:01] a graça de deus é suficiente")
	if err != nil {
		t.Fatalf("Fase2Candidatos: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("esperava 2 candidatos, veio %d", len(cands))
	}
	if cands[0].FraseAncora != "a graça de deus é suficiente" {
		t.Errorf("frase_ancora inesperada: %q", cands[0].FraseAncora)
	}
	if cands[0].Bloco != "abertura sobre a graça" {
		t.Errorf("bloco inesperado: %q", cands[0].Bloco)
	}
	// A Fase 2 NÃO emite tempo: o tipo não tem campos de início/fim/duração.
	// (garantido em tempo de compilação pelo struct CandidatoBruto)
	// A Fase 2 recebe o MAPA embutido na mensagem de usuário, junto da transcrição.
	if !strings.Contains(fake.ultimoUsuario, "A graça suficiente de Deus") {
		t.Errorf("mapa não foi repassado à fase 2: %q", fake.ultimoUsuario)
	}
	if !strings.Contains(fake.ultimoUsuario, "TRANSCRIÇÃO") {
		t.Errorf("transcrição não foi repassada à fase 2")
	}
	if fake.ultimoMax != maxTokensCandidatos {
		t.Errorf("max_tokens da fase 2 = %d, queria %d", fake.ultimoMax, maxTokensCandidatos)
	}
}

func TestFase2SemCandidatos(t *testing.T) {
	fake := &modeloFake{resposta: `{"candidatos":[]}`}
	if _, err := Fase2Candidatos(context.Background(), fake, "P", Mapa{}, "t"); err == nil {
		t.Error("esperava erro quando nenhum candidato é identificado")
	}
}

// --- Cliente HTTP real contra um llama-server fake (httptest) ---

func envelopa(content string) string {
	env := map[string]any{
		"choices": []map[string]any{
			{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}},
		},
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestClienteLLMChamada(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		io.WriteString(w, envelopa(mapaJSON))
	}))
	defer srv.Close()

	cli := NovoClienteLLM(srv.URL)
	got, err := cli.Completar(context.Background(), "sys", "user", 1234)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if got != mapaJSON {
		t.Errorf("conteúdo devolvido inesperado")
	}

	// Confere os parâmetros fechados no spike.
	if payload["temperature"].(float64) != temperatura {
		t.Errorf("temperature = %v", payload["temperature"])
	}
	if payload["max_tokens"].(float64) != 1234 {
		t.Errorf("max_tokens = %v (deveria ser por-chamada)", payload["max_tokens"])
	}
	ctk, _ := payload["chat_template_kwargs"].(map[string]any)
	if ctk == nil || ctk["enable_thinking"] != false {
		t.Errorf("enable_thinking deve ser false: %v", payload["chat_template_kwargs"])
	}
	rf, _ := payload["response_format"].(map[string]any)
	if rf == nil || rf["type"] != "json_object" {
		t.Errorf("response_format deve ser json_object: %v", payload["response_format"])
	}
}

func TestClienteLLMConteudoVazio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, envelopa(""))
	}))
	defer srv.Close()

	if _, err := NovoClienteLLM(srv.URL).Completar(context.Background(), "s", "u", 10); err == nil || !strings.Contains(err.Error(), "vazio") {
		t.Errorf("esperava erro de conteúdo vazio, veio: %v", err)
	}
}

func TestFase1ComClienteHTTP(t *testing.T) {
	// Ponta a ponta da Fase 1 usando o cliente HTTP contra um servidor fake.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, envelopa(mapaJSON))
	}))
	defer srv.Close()

	mapa, err := Fase1Mapa(context.Background(), NovoClienteLLM(srv.URL), "PROMPT", "transcricao")
	if err != nil {
		t.Fatalf("Fase1 via HTTP: %v", err)
	}
	if len(mapa.Blocos) != 2 {
		t.Errorf("esperava 2 blocos, veio %d", len(mapa.Blocos))
	}
}
