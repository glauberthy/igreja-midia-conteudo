package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/validacao"
)

const transcricaoFake = `[00:00:01] a graça de deus é suficiente para você hoje
[00:00:11] de verdade eu vos digo o senhor é o meu pastor
[00:00:30] e nada me faltará aleluia
`

// respostaModelo é o JSON de candidatos que o modelo "fake" devolve dentro de
// message.content. Traz de propósito os erros que a correção deve resolver:
// start deslizado, hook inventado (descarte) e score fora da soma.
const respostaModelo = `{
  "mapa_sermao": { "tema": "graça" },
  "candidatos": [
    { "start": "00:00:14.000", "end": "00:00:30.000", "duration_seconds": 16,
      "score": 93, "hook": "de verdade eu vos digo", "complete_thought": true,
      "criteria": {"context_fidelity":28,"pastoral_value":29,"completeness":18,"opening_strength":9,"format_fit":9} },
    { "start": "00:00:20.000", "end": "00:00:40.000", "duration_seconds": 20,
      "score": 88, "hook": "isto jamais foi dito pelo pregador", "complete_thought": true,
      "criteria": {"context_fidelity":26,"pastoral_value":26,"completeness":18,"opening_strength":9,"format_fit":9} },
    { "start": "00:00:30.000", "end": "00:00:45.000", "duration_seconds": 15,
      "score": 80, "hook": "e nada me faltará", "complete_thought": true,
      "criteria": {"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10} }
  ]
}`

// prepararArquivos grava transcrição e prompt num diretório temporário.
func prepararArquivos(t *testing.T) (transcPath, promptPath string) {
	t.Helper()
	dir := t.TempDir()
	transcPath = filepath.Join(dir, "transc.txt")
	promptPath = filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(transcPath, []byte(transcricaoFake), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("prompt de sistema de teste"), 0644); err != nil {
		t.Fatal(err)
	}
	return transcPath, promptPath
}

// envelopa embrulha o content no formato de resposta estilo OpenAI do llama-server.
func envelopa(content string) string {
	env := map[string]any{
		"choices": []map[string]any{
			{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}},
		},
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestSelecionarAplicaCorrecao(t *testing.T) {
	transcPath, promptPath := prepararArquivos(t)

	var payloadRecebido map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payloadRecebido)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, envelopa(respostaModelo))
	}))
	defer srv.Close()

	cfg := Config{Endpoint: srv.URL, PromptPath: promptPath}
	candidatos, err := Selecionar(context.Background(), transcPath, cfg)
	if err != nil {
		t.Fatalf("Selecionar: %v", err)
	}

	// Hook inventado descartado: sobram 2.
	if len(candidatos) != 2 {
		t.Fatalf("esperava 2 candidatos, veio %d", len(candidatos))
	}

	byHook := map[string]validacao.Candidato{}
	for _, c := range candidatos {
		byHook[c.Hook] = c
	}
	if _, tem := byHook["isto jamais foi dito pelo pregador"]; tem {
		t.Error("hook inventado não foi descartado")
	}

	desl := byHook["de verdade eu vos digo"]
	if desl.Start != "00:00:11.000" {
		t.Errorf("start deslizado não corrigido: %q", desl.Start)
	}
	if int(desl.DurationSeconds) != 19 {
		t.Errorf("duração não recalculada: %v", desl.DurationSeconds)
	}
	if byHook["e nada me faltará"].Score != 100 {
		t.Errorf("score não recalculado: %v", byHook["e nada me faltará"].Score)
	}

	// Confere que o payload levou os parâmetros fechados no spike.
	if payloadRecebido["temperature"].(float64) != temperatura {
		t.Errorf("temperature inesperada: %v", payloadRecebido["temperature"])
	}
	ctk, _ := payloadRecebido["chat_template_kwargs"].(map[string]any)
	if ctk == nil || ctk["enable_thinking"] != false {
		t.Errorf("enable_thinking deve ser false: %v", payloadRecebido["chat_template_kwargs"])
	}
}

func TestSelecionarConteudoVazio(t *testing.T) {
	transcPath, promptPath := prepararArquivos(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, envelopa(""))
	}))
	defer srv.Close()

	_, err := Selecionar(context.Background(), transcPath, Config{Endpoint: srv.URL, PromptPath: promptPath})
	if err == nil || !strings.Contains(err.Error(), "vazio") {
		t.Errorf("esperava erro de conteúdo vazio, veio: %v", err)
	}
}

func TestSelecionarJSONInvalido(t *testing.T) {
	transcPath, promptPath := prepararArquivos(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, envelopa("isto não é json"))
	}))
	defer srv.Close()

	_, err := Selecionar(context.Background(), transcPath, Config{Endpoint: srv.URL, PromptPath: promptPath})
	if err == nil || !strings.Contains(err.Error(), "JSON inválido") {
		t.Errorf("esperava erro de JSON inválido, veio: %v", err)
	}
}
