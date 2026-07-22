package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
)

// spec-09: a flag -cand explícita SEMPRE vence o caminho padrão.
func TestCaminhoCandFlagVenceOPadrao(t *testing.T) {
	if got := caminhoCandidatos("trabalho", "sermao", "/outro/lugar/cands.json"); got != "/outro/lugar/cands.json" {
		t.Errorf("-cand explícito deveria vencer, veio %q", got)
	}
	want := filepath.Join("trabalho", "sermao", "candidatos.corrigido.json")
	if got := caminhoCandidatos("trabalho", "sermao", ""); got != want {
		t.Errorf("sem -cand deveria usar o padrão %q, veio %q", want, got)
	}
}

// spec-09: ausência de arquivo → erro, nunca fallback para outra fonte.
func TestCarregarCandidatosAusenteErro(t *testing.T) {
	if _, err := carregarCandidatos(filepath.Join(t.TempDir(), "nao_existe.json")); err == nil {
		t.Error("arquivo ausente deveria dar erro, não fallback")
	}
}

// spec-09: arquivo válido mas sem candidatos → erro (não renderiza vazio).
func TestCarregarCandidatosVazioErro(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vazio.json")
	if err := os.WriteFile(p, []byte(`{"candidatos":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := carregarCandidatos(p); err == nil {
		t.Error("arquivo sem candidatos deveria dar erro")
	}
}

func TestCarregarCandidatosLeDoArquivo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cands.json")
	doc := `{"candidatos":[
	  {"start":"01:36:51.000","end":"01:37:25.000","duration_seconds":34,"score":92,"hook":"validado"}
	]}`
	if err := os.WriteFile(p, []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}
	cs, err := carregarCandidatos(p)
	if err != nil {
		t.Fatalf("carregarCandidatos: %v", err)
	}
	if len(cs) != 1 || cs[0].Hook != "validado" || cs[0].Score != 92 {
		t.Errorf("candidatos inesperados: %+v", cs)
	}
}

// spec-09: um pedido.json legado com candidatos embutidos NÃO os expõe como fonte —
// o campo saiu do Pedido, então Carregar simplesmente ignora o campo antigo e Salvar
// não o regrava. A fonte de verdade é o arquivo validado, nunca o pedido.
func TestPedidoLegadoIgnoraCandidatosEmbutidos(t *testing.T) {
	base := t.TempDir()
	id := "legado"
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// pedido.json ao estilo pré-spec-09, com 5 candidatos antigos embutidos.
	legado := `{
	  "id": "legado", "youtube_url": "u", "inicio": "01:29:38", "fim": "02:05:11",
	  "status": "concluido", "criado_em": "2026-07-21T10:30:00Z",
	  "candidatos": [
	    {"start":"01:37:29.000","end":"01:37:43.000","duration_seconds":14,"score":95,"hook":"antigo"}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "pedido.json"), []byte(legado), 0644); err != nil {
		t.Fatal(err)
	}

	ped, err := pipeline.Carregar(base, id)
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}
	// O campo saiu do struct: os candidatos embutidos não têm como ser usados.
	// Regravar o pedido não deve reintroduzir "candidatos" no JSON.
	if err := ped.Salvar(base); err != nil {
		t.Fatalf("Salvar: %v", err)
	}
	regravado, err := os.ReadFile(filepath.Join(dir, "pedido.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(regravado), `"candidatos"`) {
		t.Errorf("pedido.json regravado não deveria conter candidatos:\n%s", regravado)
	}
}
