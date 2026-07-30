package servidor

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

// TestFluxoCompletoDoAjuste simula o que o operador faz, na ordem: abre a revisão, marca um
// fim 2s adiante (o caso do timestamp adiantado), confere o feedback, e confirma. Verifica
// que o render recebe exatamente aquilo.
func TestFluxoCompletoDoAjuste(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
	os.WriteFile(filepath.Join(s.baseDir, "teste-1", "transcricao.txt"), []byte(transcricaoAjuste()), 0644)

	// 1) feedback ao vivo: o operador empurrou o fim para 80s
	code, prev := postAjustar(t, s, 0, 36000, 80000)
	if code != 200 || !prev.Aprovavel {
		t.Fatalf("pré-visualização falhou: %d %s", code, prev.Motivo)
	}

	// 2) confirma pelo MESMO formato que o JS envia (formulário)
	corpo := "aprovados=0&ajuste_0=" + "36000,80000" // ms inteiros
	req := httptest.NewRequest("POST", "/pedidos/teste-1/aprovar", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("aprovar (formulário) devolveu %d: %s", w.Code, w.Body)
	}
	esperarStatus(t, s, "teste-1", "concluido")

	// 3) o render recebeu o corte do operador, idêntico ao que ele viu na pré-visualização
	rf.mu.Lock()
	got := rf.recebidos
	rf.mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("render recebeu %d", len(got))
	}
	// ATENÇÃO, LEITOR: esta comparação NÃO distingue o bug de truncagem de milissegundo que
	// custou dois Shorts cortados no meio da palavra (2026-07-30). Ela compara duas strings que
	// saem do MESMO formatador (`hms`): quando ele truncava, a tela e o render mentiam juntos, e
	// este teste continuava verde. Fica no teste, com este aviso, porque a igualdade tela=render
	// vale por si — mas quem cobre a truncagem é o TestAjusteFinoSobreviveEmMilissegundos, que
	// compara com os MILISSEGUNDOS ENVIADOS, e não com o que o servidor devolveu.
	if got[0].Start != prev.Start || got[0].End != prev.End {
		t.Errorf("render recebeu %s→%s, a tela mostrou %s→%s", got[0].Start, got[0].End, prev.Start, prev.End)
	}
	if got[0].Hook != prev.Hook {
		t.Errorf("hook divergiu entre a tela e o render")
	}
}

// TestAjusteFinoSobreviveEmMilissegundos é o teste da truncagem: o operador empurra o fim em
// 0,25 s (o passo dos botões finos) e o render tem de receber ESSE tempo, não o segundo cheio
// anterior.
//
// A referência é o valor ENVIADO no POST — não o que o servidor devolveu na pré-visualização.
// É a diferença entre verificar a propriedade e verificar a consistência interna: com o `hms`
// truncando, todos os valores devolvidos pelo servidor concordavam entre si, e só o pedido
// original discordava.
//
// O que estava em jogo: 250 ms no fim de um corte é o fim da última palavra. Dois Shorts reais
// saíram com "aquilo que to.." em vez de "aquilo que toca.".
func TestAjusteFinoSobreviveEmMilissegundos(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
	os.WriteFile(filepath.Join(s.baseDir, "teste-1", "transcricao.txt"), []byte(transcricaoAjuste()), 0644)

	const iniMs, fimMs = 36000, 80250 // 44,250 s: o fim NÃO cai num segundo cheio
	corpo := fmt.Sprintf("aprovados=0&ajuste_0=%d,%d", iniMs, fimMs)
	req := httptest.NewRequest("POST", "/pedidos/teste-1/aprovar", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("aprovar devolveu %d: %s", w.Code, w.Body)
	}
	esperarStatus(t, s, "teste-1", "concluido")

	rf.mu.Lock()
	got := append([]validacao.Candidato(nil), rf.recebidos...)
	rf.mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("render recebeu %d candidato(s)", len(got))
	}

	// A prova em MILISSEGUNDOS: converte de volta a string que o render vai usar.
	iniViu, ok1 := transcricao.HmsToMs(got[0].Start)
	fimViu, ok2 := transcricao.HmsToMs(got[0].End)
	if !ok1 || !ok2 {
		t.Fatalf("o render recebeu tempos ilegíveis: %q → %q", got[0].Start, got[0].End)
	}
	if iniViu != iniMs {
		t.Errorf("início: render recebeu %d ms (%q), o operador pediu %d ms", iniViu, got[0].Start, iniMs)
	}
	if fimViu != fimMs {
		t.Errorf("fim: render recebeu %d ms (%q), o operador pediu %d ms — %d ms de fala perdidos, "+
			"que é o fim da última palavra", fimViu, got[0].End, fimMs, fimMs-fimViu)
	}
	// E a duração que o render usa para o -t do ffmpeg.
	if quer := float64(fimMs-iniMs) / 1000; got[0].DurationSeconds != quer {
		t.Errorf("duração = %.3f s, quero %.3f s", got[0].DurationSeconds, quer)
	}
}
