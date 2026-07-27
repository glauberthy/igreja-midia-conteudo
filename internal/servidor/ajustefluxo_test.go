package servidor

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	code, prev := postAjustar(t, s, 0, 36, 80)
	if code != 200 || !prev.Aprovavel {
		t.Fatalf("pré-visualização falhou: %d %s", code, prev.Motivo)
	}

	// 2) confirma pelo MESMO formato que o JS envia (formulário)
	corpo := "aprovados=0&ajuste_0=" + "36,80"
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
	if got[0].Start != prev.Start || got[0].End != prev.End {
		t.Errorf("render recebeu %s→%s, a tela mostrou %s→%s", got[0].Start, got[0].End, prev.Start, prev.End)
	}
	if got[0].Hook != prev.Hook {
		t.Errorf("hook divergiu entre a tela e o render")
	}
}
