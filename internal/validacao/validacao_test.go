package validacao

import (
	"encoding/json"
	"testing"
)

const transcricao = `[00:00:01] a graça de deus é suficiente para você hoje
[00:00:11] de verdade eu vos digo o senhor é o meu pastor
[00:00:30] e nada me faltará aleluia
`

// candidatos exercita cada correção: cand1 ok; cand2 start deslizado;
// cand3 hook inexistente (descarta); cand4 score fora da soma.
const candidatos = `{
  "mapa_sermao": { "tema": "graça suficiente" },
  "candidatos": [
    { "start": "00:00:01.000", "end": "00:00:11.000", "duration_seconds": 10,
      "score": 100, "hook": "a graça de deus é suficiente", "complete_thought": true,
      "criteria": {"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10} },
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

func docDe(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestProcessarCorrige(t *testing.T) {
	doc := docDe(t, candidatos)
	palavras := LerTranscricao(transcricao)

	res, err := Processar(doc, palavras, true)
	if err != nil {
		t.Fatal(err)
	}

	// O hook inexistente é descartado: 3 dos 4 permanecem.
	if len(res.Mantidos) != 3 {
		t.Fatalf("esperava 3 mantidos, veio %d", len(res.Mantidos))
	}

	byHook := map[string]map[string]json.RawMessage{}
	for _, m := range res.Mantidos {
		byHook[getStr(m, "hook")] = m
	}
	if _, tem := byHook["isto jamais foi dito pelo pregador"]; tem {
		t.Error("hook inexistente não foi descartado")
	}

	// start deslizado (00:00:14) reescrito para o horário real do hook (00:00:11);
	// duração recalculada por end-start (30-11=19).
	desl := byHook["de verdade eu vos digo"]
	if got := getStr(desl, "start"); got != "00:00:11.000" {
		t.Errorf("start não corrigido: %q", got)
	}
	if dur, _ := getFloat(desl, "duration_seconds"); int(dur) != 19 {
		t.Errorf("duração não recalculada: %v", dur)
	}

	// score recalculado como soma (80 -> 100).
	if sc, _ := getFloat(byHook["e nada me faltará"], "score"); int(sc) != 100 {
		t.Errorf("score não recalculado: %v", sc)
	}
}

func TestProcessarDetectaCampoAusente(t *testing.T) {
	semScore := `{"candidatos":[
		{ "start":"00:00:01.000","end":"00:00:11.000","duration_seconds":10,
		  "hook":"a graça de deus é suficiente","complete_thought":true,
		  "criteria":{"context_fidelity":30,"pastoral_value":30,"completeness":20,"opening_strength":10,"format_fit":10} }
	]}`
	res, err := Processar(docDe(t, semScore), LerTranscricao(transcricao), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total == 0 {
		t.Fatal("esperava detectar problema (score ausente)")
	}
	achou := false
	for _, p := range res.Candidatos[0].Problemas {
		if p == "falta o campo 'score'" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("não reportou score ausente: %v", res.Candidatos[0].Problemas)
	}
}

func TestProcessarSemCandidatos(t *testing.T) {
	if _, err := Processar(docDe(t, `{"mapa_sermao":{}}`), nil, false); err == nil {
		t.Error("esperava erro para documento sem 'candidatos'")
	}
}

func TestNormalizar(t *testing.T) {
	if got := Normalizar("A Graça, de DEUS!"); got != "a graca de deus" {
		t.Errorf("Normalizar = %q", got)
	}
}

func TestLerTranscricao(t *testing.T) {
	ps := LerTranscricao(transcricao)
	if len(ps) == 0 {
		t.Fatal("não leu palavras")
	}
	if ps[0].Txt != "a" || ps[0].Ms != 1000 {
		t.Errorf("primeira palavra inesperada: %+v", ps[0])
	}
}

func TestAcharHook(t *testing.T) {
	ps := LerTranscricao(transcricao)
	if ms, ok := AcharHook(ps, "de verdade eu vos digo", 14000); !ok || ms != 11000 {
		t.Errorf("AcharHook real = %d,%v; queria 11000,true", ms, ok)
	}
	if _, ok := AcharHook(ps, "isto jamais foi dito", 0); ok {
		t.Error("AcharHook devia falhar para hook inexistente")
	}
}
