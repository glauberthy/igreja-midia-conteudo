package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// transcricaoExemplo casa com os hooks usados nos candidatos de teste.
const transcricaoExemplo = `[00:00:01] a graça de deus é suficiente para você hoje
[00:00:11] de verdade eu vos digo o senhor é o meu pastor
[00:00:30] e nada me faltará aleluia
`

// candidatosExemplo exercita cada correção do validador:
// cand1 = tudo certo; cand2 = start deslizado; cand3 = hook inexistente (descarta);
// cand4 = score fora da soma dos critérios.
const candidatosExemplo = `{
  "mapa_sermao": { "tema": "graça suficiente" },
  "candidatos": [
    {
      "start": "00:00:01.000", "end": "00:00:11.000", "duration_seconds": 10,
      "score": 100, "hook": "a graça de deus é suficiente", "complete_thought": true,
      "criteria": {"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10}
    },
    {
      "start": "00:00:14.000", "end": "00:00:30.000", "duration_seconds": 16,
      "score": 93, "hook": "de verdade eu vos digo", "complete_thought": true,
      "criteria": {"context_fidelity":28,"pastoral_value":29,"completeness":18,"opening_strength":9,"format_fit":9}
    },
    {
      "start": "00:00:20.000", "end": "00:00:40.000", "duration_seconds": 20,
      "score": 88, "hook": "isto jamais foi dito pelo pregador", "complete_thought": true,
      "criteria": {"context_fidelity":26,"pastoral_value":26,"completeness":18,"opening_strength":9,"format_fit":9}
    },
    {
      "start": "00:00:30.000", "end": "00:00:45.000", "duration_seconds": 15,
      "score": 80, "hook": "e nada me faltará", "complete_thought": true,
      "criteria": {"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10}
    }
  ]
}`

// escrevePar grava transcrição + candidatos num diretório temporário e devolve os caminhos.
func escrevePar(t *testing.T, transc, cands string) (jsonPath, transcPath string) {
	t.Helper()
	dir := t.TempDir()
	transcPath = filepath.Join(dir, "transc.txt")
	jsonPath = filepath.Join(dir, "candidatos.json")
	if err := os.WriteFile(transcPath, []byte(transc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, []byte(cands), 0644); err != nil {
		t.Fatal(err)
	}
	return jsonPath, transcPath
}

// semStdout executa fn silenciando a saída padrão (validarPar imprime bastante)
// e devolve o que foi impresso, para as asserções de detecção.
func semStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func TestValidarCorrigeTudo(t *testing.T) {
	jsonPath, transcPath := escrevePar(t, transcricaoExemplo, candidatosExemplo)

	corrigir = true
	defer func() { corrigir = false }()
	semStdout(t, func() { validarPar(jsonPath, transcPath, 0) })

	out := strings.TrimSuffix(jsonPath, ".json") + ".corrigido.json"
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("não gerou o .corrigido.json: %v", err)
	}

	var doc struct {
		MapaSermao map[string]any             `json:"mapa_sermao"`
		Candidatos []map[string]json.RawMessage `json:"candidatos"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("corrigido inválido: %v", err)
	}

	// mapa_sermao preservado.
	if doc.MapaSermao["tema"] != "graça suficiente" {
		t.Errorf("mapa_sermao não preservado: %v", doc.MapaSermao)
	}
	// Hook inexistente foi descartado: sobram 3 dos 4.
	if len(doc.Candidatos) != 3 {
		t.Fatalf("esperava 3 candidatos mantidos, veio %d", len(doc.Candidatos))
	}

	byHook := map[string]map[string]json.RawMessage{}
	for _, c := range doc.Candidatos {
		byHook[getStr(c, "hook")] = c
	}
	if _, temInventado := byHook["isto jamais foi dito pelo pregador"]; temInventado {
		t.Error("candidato com hook inexistente não foi descartado")
	}

	// Start deslizado reescrito para o horário real do hook (00:00:11).
	desl := byHook["de verdade eu vos digo"]
	if desl == nil {
		t.Fatal("candidato do start deslizado sumiu")
	}
	if got := getStr(desl, "start"); !strings.HasPrefix(got, "00:00:11") {
		t.Errorf("start deslizado não corrigido: %q", got)
	}
	// duration_seconds recalculada por end-start (30-11 = 19).
	if dur, ok := getFloat(desl, "duration_seconds"); !ok || int(dur) != 19 {
		t.Errorf("duration não recalculada: got %v", dur)
	}

	// score recalculado como soma dos critérios (80 -> 100).
	fim := byHook["e nada me faltará"]
	if sc, ok := getFloat(fim, "score"); !ok || int(sc) != 100 {
		t.Errorf("score não recalculado pela soma: got %v", sc)
	}
}

func TestValidarDetectaCampoObrigatorioAusente(t *testing.T) {
	// Candidato sem "score" — hook válido para não confundir com descarte.
	semScore := `{
      "mapa_sermao": {},
      "candidatos": [
        { "start": "00:00:01.000", "end": "00:00:11.000", "duration_seconds": 10,
          "hook": "a graça de deus é suficiente", "complete_thought": true,
          "criteria": {"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10} }
      ]
    }`
	jsonPath, transcPath := escrevePar(t, transcricaoExemplo, semScore)

	corrigir = false
	var problemas int
	saida := semStdout(t, func() { problemas = validarPar(jsonPath, transcPath, 0) })

	if problemas == 0 {
		t.Fatal("esperava detectar problema (campo score ausente)")
	}
	if !strings.Contains(saida, "falta o campo 'score'") {
		t.Errorf("não reportou o campo ausente; saída: %q", saida)
	}
}

func TestNormalizar(t *testing.T) {
	got := normalizar("A Graça, de DEUS!")
	if got != "a graca de deus" {
		t.Errorf("normalizar = %q, queria %q", got, "a graca de deus")
	}
}

func TestLerTranscricao(t *testing.T) {
	palavras := lerTranscricao(transcricaoExemplo)
	if len(palavras) == 0 {
		t.Fatal("não leu nenhuma palavra")
	}
	if palavras[0].txt != "a" || palavras[0].ms != 1000 {
		t.Errorf("primeira palavra inesperada: %+v", palavras[0])
	}
}

func TestAcharHook(t *testing.T) {
	palavras := lerTranscricao(transcricaoExemplo)

	// Hook real: acha o horário certo (00:00:11 = 11000ms), mesmo com start deslizado.
	ms, achou := acharHook(palavras, "de verdade eu vos digo", 14000)
	if !achou || ms != 11000 {
		t.Errorf("acharHook real = %d,%v; queria 11000,true", ms, achou)
	}
	// Hook inventado: não encontra.
	if _, achou := acharHook(palavras, "isto jamais foi dito", 0); achou {
		t.Error("acharHook devia falhar para hook inexistente")
	}
}
