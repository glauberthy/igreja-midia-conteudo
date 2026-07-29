package videocache

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Este teste existe por causa do bug mais caro deste projeto, que apareceu DUAS vezes:
//
//	`cmd/render -id <pedido do servidor>` gerava Shorts da cena errada, deslocados em 49 min,
//	com a DURAÇÃO CORRETA — nenhum sinal de erro.
//
// A causa nunca foi um valor errado. Foi um SEGUNDO LUGAR interpretando "a que instante do
// vídeo este arquivo corresponde": primeiro o render supondo ped.Inicio, depois o download
// declarando numa cópia enquanto o servidor reafirmava.
//
// A regra que fecha a classe: **quem vai cortar vídeo pergunta ao Localizar.** Ninguém mais lê
// a origem. O risco real não é o valor divergir — é alguém, meses depois, achar mais direto ler
// ped.OrigemMs e contornar o resolvedor sem perceber que está reabrindo o bug.
//
// Então o teste varre o CÓDIGO DE PRODUÇÃO (via AST, não regex) procurando leitura de origem
// fora dos lugares onde ela é legítima. É o mesmo espírito do js_referencias_test.go: verificar
// a INTEGRIDADE de uma referência, não a presença de uma string.

// lugaresQuePodemLerOrigem é a lista de permissão, com o motivo de cada um. Acrescentar um item
// aqui é uma decisão consciente — que é exatamente o ponto.
var lugaresQuePodemLerOrigem = map[string]string{
	// A definição do próprio campo e do acessor.
	"internal/pipeline/pedido.go": "define OrigemMs e Origem(); é a fonte do dado",
	// O RESOLVEDOR. É o único que combina arquivo + origem e responde a pergunta.
	"internal/videocache/videocache.go": "é o resolvedor único (Localizar)",
}

// TestSoOResolvedorLeAOrigem falha se qualquer arquivo de produção fora da lista ler
// ped.OrigemMs ou chamar ped.Origem().
func TestSoOResolvedorLeAOrigem(t *testing.T) {
	raiz := raizDoModulo(t)
	var infracoes []string

	err := filepath.WalkDir(raiz, func(caminho string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Não varre .git, docs, testdata nem pastas de trabalho.
			switch d.Name() {
			case ".git", "docs", "testdata", "trabalho", "finalizados", "resultados", "videos", "assets":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(caminho, ".go") || strings.HasSuffix(caminho, "_test.go") {
			return nil // PRODUÇÃO só: testes legitimamente inspecionam o dado
		}
		rel, _ := filepath.Rel(raiz, caminho)
		rel = filepath.ToSlash(rel)
		if _, permitido := lugaresQuePodemLerOrigem[rel]; permitido {
			return nil
		}

		fset := token.NewFileSet()
		arq, err := parser.ParseFile(fset, caminho, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		// LER A RESPOSTA DO RESOLVEDOR É O CERTO, não a infração. `fonte.OrigemMs`, com `fonte`
		// vindo de Localizar, é exatamente o uso que se quer. Então o teste primeiro descobre
		// quais variáveis do arquivo saíram de um Localizar(...) e libera o acesso NELAS.
		//
		// É uma checagem sintática e local (não há inferência de tipos aqui), e a consequência
		// de errar é conhecida: um nome não rastreado vira falso positivo, e o conserto é
		// nomear a variável a partir do Localizar. Nunca vira falso NEGATIVO, que é o que
		// importaria.
		daFonte := varsVindasDoLocalizar(arq)
		ast.Inspect(arq, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "OrigemMs" && sel.Sel.Name != "Origem" {
				return true
			}
			if base, ok := sel.X.(*ast.Ident); ok && daFonte[base.Name] {
				return true // é a resposta do resolvedor sendo consumida
			}
			pos := fset.Position(sel.Sel.NamePos)
			infracoes = append(infracoes, rel+":"+itoa(pos.Line)+" lê ."+sel.Sel.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo o código: %v", err)
	}

	if len(infracoes) > 0 {
		t.Errorf("estes lugares leem a origem do vídeo por fora do resolvedor:\n  %s\n\n"+
			"Quem precisa cortar vídeo chama videocache.Localizar, que devolve ARQUIVO e ORIGEM "+
			"juntos. Ler a origem por fora recria a classe de bug que produziu Shorts da cena "+
			"errada com a duração certa (spec-09) — duas vezes.\n\n"+
			"Se este novo leitor for legítimo (não é para renderizar), acrescente-o a "+
			"lugaresQuePodemLerOrigem COM O MOTIVO. A lista ser explícita é o valor do teste.",
			strings.Join(infracoes, "\n  "))
	}
}

// TestOsLugaresPermitidosExistem impede que a lista de permissão apodreça: um caminho que não
// existe mais daria uma falsa sensação de cobertura (o teste "protege" um arquivo inexistente e
// ninguém nota que o real ficou de fora).
func TestOsLugaresPermitidosExistem(t *testing.T) {
	raiz := raizDoModulo(t)
	for rel, motivo := range lugaresQuePodemLerOrigem {
		if _, err := os.Stat(filepath.Join(raiz, rel)); err != nil {
			t.Errorf("a lista de permissão cita %s (%q), que não existe mais: atualize a lista",
				rel, motivo)
		}
	}
}

// varsVindasDoLocalizar devolve os nomes de variáveis do arquivo que recebem o resultado de uma
// chamada a Localizar(...) — em `x, err := ...Localizar(...)` ou `x, err = ...Localizar(...)`.
// São as que podem legitimamente ler .OrigemMs: elas SÃO a resposta do resolvedor.
func varsVindasDoLocalizar(arq *ast.File) map[string]bool {
	nomes := map[string]bool{}
	registrar := func(lhs []ast.Expr, rhs []ast.Expr) {
		if len(rhs) != 1 {
			return
		}
		chamada, ok := rhs[0].(*ast.CallExpr)
		if !ok {
			return
		}
		fn, ok := chamada.Fun.(*ast.SelectorExpr)
		if !ok || fn.Sel.Name != "Localizar" {
			return
		}
		for _, e := range lhs {
			if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
				nomes[id.Name] = true
			}
		}
	}
	ast.Inspect(arq, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			registrar(a.Lhs, a.Rhs)
		}
		return true
	})
	return nomes
}

// raizDoModulo sobe do diretório do teste até achar o go.mod.
func raizDoModulo(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			break
		}
		dir = pai
	}
	t.Fatal("não achei o go.mod subindo do diretório do teste")
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
