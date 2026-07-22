package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"srtclean/internal/validacao"
)

// Padrões e nome da variável de ambiente. A chave de API, se usada (modo externo),
// vem SÓ do ambiente — nunca de arquivo, log ou código (BRD RN-038).
const (
	EndpointPadrao = "http://localhost:8080/v1/chat/completions"
	PromptPadrao   = "prompts/selecao_shorts_v0_1.md"
	EnvAPIKey      = "LLM_API_KEY"
)

// Parâmetros fechados no spike (docs/aprendizados-do-spike.md) — não reabrir.
const (
	temperatura   = 0.2
	maxTokens     = 3000
	repeatPenalty = 1.1
)

// Config parametriza a chamada ao modelo. Campos vazios recebem os padrões.
type Config struct {
	Endpoint   string       // padrão EndpointPadrao
	PromptPath string       // padrão PromptPadrao
	HTTPClient *http.Client // padrão http.Client com timeout
}

func (c *Config) aplicarPadroes() {
	if c.Endpoint == "" {
		c.Endpoint = EndpointPadrao
	}
	if c.PromptPath == "" {
		c.PromptPath = PromptPadrao
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
}

// respostaChat é o recorte que nos interessa da resposta estilo OpenAI do llama-server.
type respostaChat struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Selecionar roda o fluxo da seleção a partir de uma transcrição JÁ limpa:
// monta o payload, chama o llama-server, recebe o JSON de candidatos e aplica a
// correção determinística (internal/validacao). Devolve os candidatos já corrigidos.
//
// Erros são claros quando o modelo devolve conteúdo vazio ou JSON inválido.
func Selecionar(ctx context.Context, transcricaoPath string, cfg Config) ([]validacao.Candidato, error) {
	cfg.aplicarPadroes()

	transcRaw, err := os.ReadFile(transcricaoPath)
	if err != nil {
		return nil, fmt.Errorf("lendo transcrição %q: %w", transcricaoPath, err)
	}
	sysPrompt, err := os.ReadFile(cfg.PromptPath)
	if err != nil {
		return nil, fmt.Errorf("lendo prompt %q: %w", cfg.PromptPath, err)
	}

	conteudo, err := chamarModelo(ctx, cfg, string(sysPrompt), string(transcRaw))
	if err != nil {
		return nil, err
	}

	// O content é, ele mesmo, o JSON do documento de candidatos.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(conteudo), &doc); err != nil {
		return nil, fmt.Errorf("modelo devolveu JSON inválido: %w", err)
	}

	palavras := validacao.LerTranscricao(string(transcRaw))
	res, err := validacao.Processar(doc, palavras, true)
	if err != nil {
		return nil, fmt.Errorf("processando candidatos: %w", err)
	}

	candidatos := make([]validacao.Candidato, 0, len(res.Mantidos))
	for _, m := range res.Mantidos {
		b, _ := json.Marshal(m)
		var c validacao.Candidato
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("convertendo candidato: %w", err)
		}
		candidatos = append(candidatos, c)
	}
	return candidatos, nil
}

// chamarModelo monta o payload com os parâmetros do spike, faz o POST e extrai o
// content. A chave de API (se houver) vai só no header, nunca é logada.
func chamarModelo(ctx context.Context, cfg Config, sysPrompt, transcricao string) (string, error) {
	payload := map[string]any{
		"temperature":          temperatura,
		"max_tokens":           maxTokens,
		"repeat_penalty":       repeatPenalty,
		"response_format":      map[string]any{"type": "json_object"},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"messages": []map[string]any{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": transcricao},
		},
	}
	corpo, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("montando payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(corpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if chave := os.Getenv(EnvAPIKey); chave != "" {
		req.Header.Set("Authorization", "Bearer "+chave)
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chamando o modelo em %s: %w", cfg.Endpoint, err)
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
