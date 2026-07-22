package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// max_tokens por fase. Qualidade > tempo (spec-07): a fase de mapa precisa de mais
// fôlego (lista de blocos); a de candidatos, um pouco menos.
const (
	maxTokensMapa       = 3500
	maxTokensCandidatos = 2500
)

// BlocoEnsino é uma ideia/assunto delimitado no sermão, com bordas APROXIMADAS
// (o refino fino de tempo é da Fase 3, que é 100% código — aqui não).
type BlocoEnsino struct {
	Assunto     string `json:"assunto"`
	InicioAprox string `json:"inicio_aprox"` // [HH:MM:SS] aproximado
	FimAprox    string `json:"fim_aprox"`
}

// Mapa é a saída da Fase 1: compreensão global do sermão, sem escolher Short.
type Mapa struct {
	TemaCentral string        `json:"tema_central"`
	Estrutura   Estrutura     `json:"estrutura"`
	Blocos      []BlocoEnsino `json:"blocos"`
}

// Estrutura é o esboço/tópicos do sermão (campo puramente descritivo). O prompt pede
// uma lista de strings — e é o que o modelo devolve na maioria das vezes — mas às vezes
// ele devolve uma lista de OBJETOS ({"titulo": "..."}) ou até um único valor. Como este
// campo não alimenta nenhuma lógica (só descreve), o parse é TOLERANTE: qualquer dessas
// formas vira uma lista de strings legíveis, para a Fase 1 nunca quebrar por causa disso.
type Estrutura []string

// UnmarshalJSON aceita: lista de strings (caso normal), lista de objetos, valor único
// (string ou objeto) — sempre reduzindo a uma []string. É idempotente para o caso normal.
func (e *Estrutura) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*e = nil
		return nil
	}
	// Caso normal e o de lista-de-objetos: qualquer array.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, el := range arr {
			out = append(out, rawParaTexto(el))
		}
		*e = out
		return nil
	}
	// Valor único (string ou objeto): vira lista de um elemento.
	*e = []string{rawParaTexto(data)}
	return nil
}

// rawParaTexto reduz um valor JSON a um texto legível: string vira ela mesma; objeto
// vira a junção de seus valores string (preferindo campos de título usuais, com ordem
// determinística); qualquer outra coisa, o JSON cru aparado.
func rawParaTexto(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		for _, k := range []string{"titulo", "título", "ponto", "nome", "assunto", "descricao", "descrição", "texto"} {
			if vs, ok := obj[k].(string); ok && vs != "" {
				return vs
			}
		}
		// Sem campo conhecido: junta todos os valores string, em ordem de chave (determinístico).
		chaves := make([]string, 0, len(obj))
		for k := range obj {
			chaves = append(chaves, k)
		}
		sort.Strings(chaves)
		var partes []string
		for _, k := range chaves {
			if vs, ok := obj[k].(string); ok && vs != "" {
				partes = append(partes, vs)
			}
		}
		if len(partes) > 0 {
			return strings.Join(partes, " — ")
		}
	}
	return strings.TrimSpace(string(raw))
}

// CandidatoBruto é a saída da Fase 2: um bloco escolhido para virar Short e a
// frase-âncora (o hook pretendido). NÃO carrega tempo — nem início/fim nem duração:
// toda a delimitação de tempo é da Fase 3 (código). Estimar tempo é justamente a
// tarefa em que o modelo falha (viu-se 7s/19s/65s no teste real do sermão).
type CandidatoBruto struct {
	Bloco       string `json:"bloco"`        // referência ao bloco do mapa (assunto)
	FraseAncora string `json:"frase_ancora"` // o hook pretendido (a frase-núcleo do trecho)
	Motivo      string `json:"motivo,omitempty"`
}

// Fase1Mapa (1 chamada): lê a transcrição inteira e devolve o mapa do sermão.
// promptSistema é o conteúdo de prompts/fase1_mapa.md. A resposta passa pela rede de
// retry (spec-08): formato/campos inválidos são refeitos até MaxTentativas.
func Fase1Mapa(ctx context.Context, modelo ModeloLLM, promptSistema, transcricao string) (Mapa, error) {
	conteudo, err := PedirValidado(ctx, modelo, "Fase 1", promptSistema, transcricao, maxTokensMapa, validaMapa)
	if err != nil {
		return Mapa{}, err
	}
	var m Mapa
	_ = json.Unmarshal([]byte(conteudo), &m) // já validado por validaMapa
	return m, nil
}

// validaMapa é a validação de FORMATO da Fase 1 (spec-08): JSON parseável em Mapa, com
// tema_central não vazio e ao menos 1 bloco. Não julga qualidade do mapa.
func validaMapa(b []byte) error {
	var m Mapa
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	if m.TemaCentral == "" {
		return fmt.Errorf("faltando tema_central")
	}
	if len(m.Blocos) == 0 {
		return fmt.Errorf("nenhum bloco de ensino")
	}
	return nil
}

// Fase2Candidatos (1 chamada): dado o mapa + a transcrição, decide quais blocos
// viram Short, com limites aproximados e o hook pretendido. Sem avaliação.
// promptSistema é o conteúdo de prompts/fase2_candidatos.md.
func Fase2Candidatos(ctx context.Context, modelo ModeloLLM, promptSistema string, mapa Mapa, transcricao string) ([]CandidatoBruto, error) {
	mapaJSON, err := json.MarshalIndent(mapa, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("fase 2 (candidatos): serializando mapa: %w", err)
	}
	usuario := fmt.Sprintf("## MAPA DO SERMÃO (Fase 1)\n%s\n\n## TRANSCRIÇÃO\n%s", string(mapaJSON), transcricao)

	conteudo, err := PedirValidado(ctx, modelo, "Fase 2", promptSistema, usuario, maxTokensCandidatos, validaCandidatos)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Candidatos []CandidatoBruto `json:"candidatos"`
	}
	_ = json.Unmarshal([]byte(conteudo), &doc) // já validado por validaCandidatos
	return doc.Candidatos, nil
}

// validaCandidatos é a validação de FORMATO da Fase 2 (spec-08): JSON com `candidatos`
// não vazio, cada um com `bloco` e `frase_ancora` não vazios. Não julga se o trecho é
// bom — só se veio no formato esperado (candidatos vazios = resposta malformada).
func validaCandidatos(b []byte) error {
	var doc struct {
		Candidatos []CandidatoBruto `json:"candidatos"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	if len(doc.Candidatos) == 0 {
		return fmt.Errorf("nenhum candidato")
	}
	for i, c := range doc.Candidatos {
		if strings.TrimSpace(c.Bloco) == "" || strings.TrimSpace(c.FraseAncora) == "" {
			return fmt.Errorf("candidato %d sem bloco ou frase_ancora", i)
		}
	}
	return nil
}
