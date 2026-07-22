// Pacote harness implementa a seleção de Shorts em FASES focadas (spec-07): cada
// chamada ao modelo faz uma tarefa pequena, para não sobrecarregar o modelo local.
//
// Esta etapa traz só as Fases 1 (Mapa do sermão) e 2 (Identificação de candidatos).
// As fases 3–5 e a substituição do internal/pipeline.Selecionar são incrementos
// futuros — aqui validamos a hipótese das fases focadas antes de construir o resto.
//
// Princípio (não reabrir): o modelo só faz o que exige julgamento; timestamp,
// contagem, soma e faixa são sempre do código.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Endpoint padrão do llama-server local e a variável de ambiente da chave de API.
const (
	EndpointPadrao = "http://localhost:8080/v1/chat/completions"
	EnvAPIKey      = "LLM_API_KEY"
)

// Parâmetros fechados no spike (docs/aprendizados-do-spike.md) — não reabrir.
// max_tokens é dimensionado POR FASE (ver fases.go).
const (
	temperatura   = 0.2
	repeatPenalty = 1.1
)

// ModeloLLM é a costura mockável para o modelo: uma tarefa focada por chamada.
// Devolve o conteúdo (que as fases esperam ser um JSON) já como string.
type ModeloLLM interface {
	Completar(ctx context.Context, sistema, usuario string, maxTokens int) (string, error)
}

// ClienteLLM fala com o llama-server (API estilo OpenAI). Implementa ModeloLLM.
type ClienteLLM struct {
	Endpoint string
	HTTP     *http.Client
}

// NovoClienteLLM cria um cliente com o endpoint dado (vazio = padrão local).
func NovoClienteLLM(endpoint string) *ClienteLLM {
	if endpoint == "" {
		endpoint = EndpointPadrao
	}
	return &ClienteLLM{Endpoint: endpoint, HTTP: &http.Client{Timeout: 10 * time.Minute}}
}

type respostaChat struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Completar monta o payload com os parâmetros do spike e devolve o content.
// A chave de API (se houver) vai só no header, nunca é logada (BRD RN-038).
func (c *ClienteLLM) Completar(ctx context.Context, sistema, usuario string, maxTokens int) (string, error) {
	cliente := c.HTTP
	if cliente == nil {
		cliente = &http.Client{Timeout: 10 * time.Minute}
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = EndpointPadrao
	}

	payload := map[string]any{
		"temperature":          temperatura,
		"repeat_penalty":       repeatPenalty,
		"max_tokens":           maxTokens,
		"response_format":      map[string]any{"type": "json_object"},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"messages": []map[string]any{
			{"role": "system", "content": sistema},
			{"role": "user", "content": usuario},
		},
	}
	corpo, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("montando payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(corpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if chave := os.Getenv(EnvAPIKey); chave != "" {
		req.Header.Set("Authorization", "Bearer "+chave)
	}

	resp, err := cliente.Do(req)
	if err != nil {
		return "", fmt.Errorf("chamando o modelo em %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("modelo respondeu HTTP %d", resp.StatusCode)
	}

	var rc respostaChat
	if err := json.NewDecoder(resp.Body).Decode(&rc); err != nil {
		return "", fmt.Errorf("decodificando resposta do modelo: %w", err)
	}
	if len(rc.Choices) == 0 {
		return "", fmt.Errorf("modelo não devolveu nenhuma escolha")
	}
	conteudo := rc.Choices[0].Message.Content
	if conteudo == "" {
		return "", fmt.Errorf("modelo devolveu conteúdo vazio (finish_reason=%q)", rc.Choices[0].FinishReason)
	}
	return conteudo, nil
}
