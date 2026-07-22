package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/validacao"
)

func crit(fid, pas, com, ab, fo int) validacao.Criteria {
	return validacao.Criteria{ContextFidelity: fid, PastoralValue: pas, Completeness: com, OpeningStrength: ab, FormatFit: fo}
}

// --- Fase 4: combinação das duas rodadas ---

func TestCombinarConcordante(t *testing.T) {
	a := Avaliacao{Criteria: crit(28, 28, 18, 9, 9)} // soma 92
	b := Avaliacao{Criteria: crit(27, 29, 18, 9, 9)} // soma 92, fidelidade difere 1
	r := CombinarAvaliacoes(a, b)
	if r.Vetado {
		t.Error("não deveria vetar (fidelidade alta nas duas)")
	}
	if r.RequerRevisao {
		t.Error("não deveria marcar revisão (fidelidade próxima, mesmo veredito)")
	}
	if r.Score != 92 {
		t.Errorf("score = %d, queria 92 (menor soma)", r.Score)
	}
}

func TestCombinarScoreMenor(t *testing.T) {
	a := Avaliacao{Criteria: crit(30, 30, 20, 10, 10)} // 100
	b := Avaliacao{Criteria: crit(25, 25, 18, 8, 8)}   // 84
	r := CombinarAvaliacoes(a, b)
	if r.Score != 84 {
		t.Errorf("score = %d, queria 84 (a menor)", r.Score)
	}
	if r.Criteria.ContextFidelity != 25 {
		t.Errorf("critérios deveriam vir da rodada de menor soma: %+v", r.Criteria)
	}
}

func TestCombinarDivergenciaFidelidadeMarcaRevisao(t *testing.T) {
	a := Avaliacao{Criteria: crit(28, 28, 18, 9, 9)} // fidelidade 28
	b := Avaliacao{Criteria: crit(19, 28, 18, 9, 9)} // fidelidade 19 (difere 9 > 8)
	r := CombinarAvaliacoes(a, b)
	if !r.RequerRevisao {
		t.Error("fidelidade divergindo > 8 deveria marcar requer_revisao_reforcada")
	}
	if r.Vetado {
		t.Error("ambas acima do veto (>=18): não deveria vetar")
	}
}

func TestCombinarVetoDiscordanteMarcaRevisaoEVeta(t *testing.T) {
	a := Avaliacao{Criteria: crit(24, 28, 18, 9, 9)} // aprova (>=18)
	b := Avaliacao{Criteria: crit(12, 28, 18, 9, 9)} // veta (<18)
	r := CombinarAvaliacoes(a, b)
	if !r.Vetado {
		t.Error("uma rodada abaixo do veto deveria vetar")
	}
	if !r.RequerRevisao {
		t.Error("vereditos de veto discordantes deveriam marcar revisão")
	}
}

func TestCombinarAmbosVetamSemRevisao(t *testing.T) {
	a := Avaliacao{Criteria: crit(10, 20, 10, 5, 5)}
	b := Avaliacao{Criteria: crit(12, 22, 12, 6, 6)} // ambas < 18 (concordam no veto)
	r := CombinarAvaliacoes(a, b)
	if !r.Vetado {
		t.Error("ambas abaixo do veto deveria vetar")
	}
	if r.RequerRevisao {
		t.Error("ambas vetam (concordam): não deveria marcar revisão por discordância")
	}
}

func TestFase4Avaliar(t *testing.T) {
	fake := &modeloFake{resposta: `{"criteria":{"context_fidelity":28,"pastoral_value":27,"completeness":18,"opening_strength":9,"format_fit":9},"observacoes":"fiel e edificante"}`}
	a, err := Fase4Avaliar(context.Background(), fake, "PROMPT_AVAL", "texto do trecho")
	if err != nil {
		t.Fatal(err)
	}
	if a.Criteria.ContextFidelity != 28 || a.Observacoes != "fiel e edificante" {
		t.Errorf("avaliação inesperada: %+v", a)
	}
	if fake.ultimoSistema != "PROMPT_AVAL" || fake.ultimoUsuario != "texto do trecho" {
		t.Errorf("prompt/trecho não repassados: sys=%q user=%q", fake.ultimoSistema, fake.ultimoUsuario)
	}
}

// --- Fase 5: validação final ---

const transcricaoFase5 = `[00:00:00] A graça de Deus é suficiente para você hoje.
[00:00:35] O Senhor é o teu pastor e nada faltará.
`

func candValido() validacao.Candidato {
	return validacao.Candidato{
		Start: "00:00:00.000", End: "00:00:35.000", DurationSeconds: 35, Score: 92,
		Hook: "A graça de Deus é suficiente para você hoje.", CompleteThought: true,
		Criteria: crit(28, 28, 18, 9, 9),
	}
}

func TestFase5MantemValido(t *testing.T) {
	oks, descs := Fase5Validar([]validacao.Candidato{candValido()}, transcricaoFase5)
	if len(oks) != 1 {
		t.Fatalf("esperava 1 aprovado, veio %d (descartes %+v)", len(oks), descs)
	}
	if oks[0].Score != 92 {
		t.Errorf("score inesperado: %d", oks[0].Score)
	}
}

func TestFase5DescartaScoreZero(t *testing.T) {
	c := candValido()
	c.Score = 0
	c.Criteria = crit(0, 0, 0, 0, 0)
	oks, descs := Fase5Validar([]validacao.Candidato{c}, transcricaoFase5)
	if len(oks) != 0 {
		t.Errorf("candidato com score 0 não deveria passar")
	}
	if len(descs) == 0 {
		t.Error("deveria registrar o descarte")
	}
}

func TestFase5DescartaDuracaoForaFaixa(t *testing.T) {
	c := candValido()
	c.End = "00:01:20.000" // 80s > 60
	oks, _ := Fase5Validar([]validacao.Candidato{c}, transcricaoFase5)
	if len(oks) != 0 {
		t.Errorf("candidato com duração fora de 30–60s não deveria passar")
	}
}

func TestFase5DescartaHookInexistente(t *testing.T) {
	c := candValido()
	c.Hook = "isto jamais foi dito nesta pregação"
	oks, descs := Fase5Validar([]validacao.Candidato{c}, transcricaoFase5)
	if len(oks) != 0 {
		t.Errorf("hook inexistente não deveria passar")
	}
	if len(descs) == 0 {
		t.Error("deveria registrar o descarte do hook inventado")
	}
}

// --- Orquestração completa (Fases 1→5) com modelo fake ---

// fakeMulti responde conforme a fase, detectada pelo conteúdo da mensagem de usuário.
type fakeMulti struct{ mapa, cands, aval string }

func (f fakeMulti) Completar(ctx context.Context, sistema, usuario string, maxTokens int) (string, error) {
	switch {
	case strings.Contains(usuario, "MAPA DO SERMÃO"):
		return f.cands, nil // Fase 2
	case strings.Contains(usuario, "["): // a transcrição traz marcadores [HH:MM:SS]
		return f.mapa, nil // Fase 1
	default:
		return f.aval, nil // Fase 4 (texto do trecho, sem timestamps)
	}
}

func TestSelecionarPontaAPonta5Fases(t *testing.T) {
	dir := t.TempDir()
	// Prompts (conteúdo é irrelevante para o fake; só precisam existir).
	for _, nome := range []string{"fase1_mapa.md", "fase2_candidatos.md", "fase4_avaliacao.md"} {
		if err := os.WriteFile(filepath.Join(dir, nome), []byte("prompt "+nome), 0644); err != nil {
			t.Fatal(err)
		}
	}
	transc := `[00:00:00] A graça de Deus é suficiente para todos nós.
[00:00:12] Ele nunca abandona quem nele confia de coração.
[00:00:24] Por isso descansa e confia no Senhor todos os dias.
[00:00:36] O amor de Cristo alcança o pecador perdido.
[00:00:48] E oferece vida nova a quem crê.
`
	transcPath := filepath.Join(dir, "transc.txt")
	if err := os.WriteFile(transcPath, []byte(transc), 0644); err != nil {
		t.Fatal(err)
	}

	fake := fakeMulti{
		mapa:  `{"tema_central":"graça","estrutura":["intro"],"blocos":[{"assunto":"graça","inicio_aprox":"00:00:00","fim_aprox":"00:01:00"}]}`,
		cands: `{"candidatos":[{"bloco":"graça","frase_ancora":"A graça de Deus é suficiente"}]}`,
		aval:  `{"criteria":{"context_fidelity":28,"pastoral_value":27,"completeness":18,"opening_strength":9,"format_fit":9},"observacoes":"fiel"}`,
	}

	cfg := Config{Modelo: fake, PromptDir: dir, DeclaracaoPath: ""}
	finais, err := Selecionar(context.Background(), transcPath, cfg)
	if err != nil {
		t.Fatalf("Selecionar: %v", err)
	}
	if len(finais) != 1 {
		t.Fatalf("esperava 1 candidato final, veio %d", len(finais))
	}
	c := finais[0]
	if c.Score <= 0 {
		t.Errorf("candidato final com score 0: %+v", c)
	}
	if c.DurationSeconds < 30 || c.DurationSeconds > 60 {
		t.Errorf("duração final fora de 30–60s: %v", c.DurationSeconds)
	}
	if c.Start != "00:00:00.000" {
		t.Errorf("start inesperado: %q", c.Start)
	}
	if c.Criteria.Soma() != c.Score {
		t.Errorf("score (%d) != soma dos critérios (%d)", c.Score, c.Criteria.Soma())
	}
}

func TestSelecionarVetaBaixaFidelidade(t *testing.T) {
	dir := t.TempDir()
	for _, nome := range []string{"fase1_mapa.md", "fase2_candidatos.md", "fase4_avaliacao.md"} {
		os.WriteFile(filepath.Join(dir, nome), []byte("p"), 0644)
	}
	transc := `[00:00:00] A graça de Deus é suficiente para todos nós.
[00:00:12] Ele nunca abandona quem nele confia de coração.
[00:00:24] Por isso descansa e confia no Senhor todos os dias.
[00:00:36] O amor de Cristo alcança o pecador perdido.
[00:00:48] E oferece vida nova a quem crê.
`
	transcPath := filepath.Join(dir, "transc.txt")
	os.WriteFile(transcPath, []byte(transc), 0644)

	// Fidelidade abaixo do veto nas duas rodadas -> reprovado -> nenhum final.
	fake := fakeMulti{
		mapa:  `{"tema_central":"graça","estrutura":["intro"],"blocos":[{"assunto":"graça","inicio_aprox":"00:00:00","fim_aprox":"00:01:00"}]}`,
		cands: `{"candidatos":[{"bloco":"graça","frase_ancora":"A graça de Deus é suficiente"}]}`,
		aval:  `{"criteria":{"context_fidelity":8,"pastoral_value":10,"completeness":8,"opening_strength":4,"format_fit":4},"observacoes":"distorce"}`,
	}
	finais, err := Selecionar(context.Background(), transcPath, Config{Modelo: fake, PromptDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(finais) != 0 {
		t.Errorf("candidato de baixa fidelidade deveria ser vetado; veio %d final(is)", len(finais))
	}
}
