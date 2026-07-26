// Pacote retencao apaga o material BRUTO dos pedidos antigos, preservando o histórico
// auditável (spec-06).
//
// Por que existe: a fase pesada baixa o vídeo INTEIRO (decisão medida da spec-05 — 7,3 s
// contra 577 s do download seccionado). O preço é disco: a auditoria mediu ~571 MB por
// pedido, 4,0 GB em 7 pedidos. Sem limpeza o disco enche e o operador recebe um erro
// incompreensível — bem longe do problema real.
//
// Política: mantém o bruto dos N pedidos mais recentes (default 1) e limpa os anteriores.
// Manter o último permite regerar um Short sem baixar de novo; manter mais que isso volta
// a acumular.
//
// O que é apagado é sempre REGENERÁVEL (baixa-se de novo); o que é preservado é o que não
// se recupera ou custa caro recuperar. Ver `removiveis` e `preservados` — e o teste que
// impede alguém de mover um arquivo de uma lista para a outra sem perceber.
package retencao

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// removiveis são os arquivos de material BRUTO, regeneráveis, que a limpeza apaga.
// Nomes exatos; para famílias de arquivos use padroesRemoviveis.
var removiveis = []string{
	"video.mp4",              // o gordo (~570 MB) — baixa de novo se precisar
	"legenda.srt",            // baixa de novo
	"legenda.info.json",      // metadados do yt-dlp
	"mapa.json",              // intermediário da Fase 1
	"candidatos_brutos.json", // intermediário da Fase 2
	"candidatos_delim.json",  // intermediário da Fase 3
}

// padroesRemoviveis são famílias de arquivos descartáveis (glob simples, sem separador).
var padroesRemoviveis = []string{
	"short_*.sub*.txt", // blocos de legenda do drawtext (resíduo; 46 num pedido auditado)
	"*.part",           // download interrompido do yt-dlp
	"*.ytdl",           // idem
}

// preservados NUNCA são apagados, mesmo que alguém os inclua em `removiveis` por engano.
// É a rede de segurança do histórico auditável — cada item aqui tem um motivo:
var preservados = []string{
	"candidatos.corrigido.json", // fonte de verdade validada (spec-09) — sem ela não se
	//                              sabe o que foi selecionado nem se renderiza de novo
	"transcricao.txt",        // ~130 KB e é insumo de auditoria (cmd/auditar, spec-16)
	"revisao-teologica.json", // veredito do confronto doutrinário (spec-14)
	"pedido.json",            // metadados do pedido (url, janela, status)
}

// Opcoes parametriza a limpeza.
type Opcoes struct {
	RaizTrabalho string   // pasta raiz dos pedidos (ex.: "trabalho")
	Reter        int      // quantos pedidos MAIS RECENTES manter intactos (mín. 1)
	Intocaveis   []string // ids que nunca podem ser tocados (ex.: o pedido em curso)
	DryRun       bool     // só relata o que faria, não apaga nada
}

// PedidoLimpo descreve o que foi (ou seria) removido de um pedido.
type PedidoLimpo struct {
	ID       string
	Arquivos []string
	Bytes    int64
}

// Resultado agrega a limpeza.
type Resultado struct {
	Pedidos        []PedidoLimpo
	BytesLiberados int64
	Retidos        []string // ids preservados pela política (os mais recentes + intocáveis)
	DryRun         bool
}

// Resumo é a linha de log para o operador.
func (r Resultado) Resumo() string {
	verbo := "liberados"
	if r.DryRun {
		verbo = "seriam liberados (dry-run)"
	}
	return fmt.Sprintf("limpeza: %d pedido(s) limpo(s), %s %s; %d pedido(s) retido(s)",
		len(r.Pedidos), FormatarBytes(r.BytesLiberados), verbo, len(r.Retidos))
}

// FormatarBytes devolve o tamanho legível (MB/GB).
func FormatarBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", b/1024)
	}
}

// Limpar aplica a política e devolve o que foi (ou seria) removido. Idempotente: rodar de
// novo não quebra nem remove o que já foi removido.
func Limpar(o Opcoes) (Resultado, error) {
	res := Resultado{DryRun: o.DryRun}
	if o.RaizTrabalho == "" {
		return res, fmt.Errorf("raiz de trabalho vazia")
	}
	reter := o.Reter
	if reter < 1 {
		reter = 1 // manter ao menos o último: regerar sem baixar de novo
	}

	pedidos, err := pedidosPorRecencia(o.RaizTrabalho)
	if err != nil {
		return res, err
	}
	intocavel := map[string]bool{}
	for _, id := range o.Intocaveis {
		intocavel[id] = true
	}

	for i, id := range pedidos {
		if i < reter || intocavel[id] {
			res.Retidos = append(res.Retidos, id)
			continue
		}
		p, err := limparPedido(o.RaizTrabalho, id, o.DryRun)
		if err != nil {
			return res, err
		}
		if len(p.Arquivos) > 0 {
			res.Pedidos = append(res.Pedidos, p)
			res.BytesLiberados += p.Bytes
		}
	}
	return res, nil
}

// pedidosPorRecencia lista os ids de pedido em `raiz`, do MAIS RECENTE para o mais antigo.
//
// A recência vem do arquivo PRESERVADO mais recente dentro do pedido — NÃO do mtime da
// pasta. Motivo (bug pego por teste): apagar arquivos ATUALIZA o mtime do diretório, então
// um pedido recém-limpo viraria "o mais recente" e, na execução seguinte, a limpeza comeria
// justamente o pedido que a política manda reter. Como os preservados nunca são apagados,
// o mtime deles é estável sob limpezas repetidas — o que torna a ordem idempotente.
// Sem nenhum preservado (pedido vazio/estranho), cai para o mtime da pasta.
func pedidosPorRecencia(raiz string) ([]string, error) {
	entradas, err := os.ReadDir(raiz)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // nada a limpar
		}
		return nil, fmt.Errorf("lendo %s: %w", raiz, err)
	}
	type item struct {
		id  string
		mod int64
	}
	var itens []item
	for _, e := range entradas {
		if !e.IsDir() {
			continue
		}
		itens = append(itens, item{e.Name(), recenciaDoPedido(filepath.Join(raiz, e.Name()))})
	}
	sort.Slice(itens, func(a, b int) bool {
		if itens[a].mod != itens[b].mod {
			return itens[a].mod > itens[b].mod
		}
		return itens[a].id > itens[b].id // desempate estável (ids têm timestamp)
	})
	ids := make([]string, len(itens))
	for i, it := range itens {
		ids[i] = it.id
	}
	return ids, nil
}

// recenciaDoPedido devolve o mtime (ns) do arquivo preservado mais recente do pedido —
// estável sob limpeza. Cai para o mtime da pasta se não houver preservados.
func recenciaDoPedido(dir string) int64 {
	var maisRecente int64
	for _, nome := range preservados {
		if fi, err := os.Stat(filepath.Join(dir, nome)); err == nil {
			if m := fi.ModTime().UnixNano(); m > maisRecente {
				maisRecente = m
			}
		}
	}
	if maisRecente == 0 {
		if fi, err := os.Stat(dir); err == nil {
			return fi.ModTime().UnixNano()
		}
	}
	return maisRecente
}

// limparPedido remove os arquivos brutos de UM pedido. Guardas de segurança antes de
// qualquer remoção — ver caminhoSeguro.
func limparPedido(raiz, id string, dryRun bool) (PedidoLimpo, error) {
	p := PedidoLimpo{ID: id}
	dir, err := caminhoSeguro(raiz, id)
	if err != nil {
		return p, err
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return p, nil // pedido sumiu no meio do caminho: nada a fazer
	}
	for _, e := range entradas {
		if e.IsDir() {
			continue // não descemos em subpastas: o bruto do pedido é plano
		}
		nome := e.Name()
		if !PodeRemover(nome) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		alvo := filepath.Join(dir, nome)
		if !dryRun {
			if err := os.Remove(alvo); err != nil {
				return p, fmt.Errorf("removendo %s: %w", alvo, err)
			}
		}
		p.Arquivos = append(p.Arquivos, nome)
		p.Bytes += fi.Size()
	}
	return p, nil
}

// PodeRemover diz se um NOME de arquivo (sem caminho) é material bruto descartável.
// A checagem de preservados vem PRIMEIRO e vence: mesmo que alguém adicione um arquivo
// protegido a `removiveis`, ele não é removido. É a trava contra o erro humano de mover
// um item de lista sem perceber (coberto por teste).
func PodeRemover(nome string) bool {
	for _, p := range preservados {
		if nome == p {
			return false
		}
	}
	for _, r := range removiveis {
		if nome == r {
			return true
		}
	}
	for _, pat := range padroesRemoviveis {
		if ok, _ := filepath.Match(pat, nome); ok {
			return true
		}
	}
	return false
}

// caminhoSeguro valida que `id` designa um pedido legítimo DENTRO de raiz e devolve o
// caminho. Recusa travessia (".."), separadores, nomes vazios/absolutos — e confere, já
// resolvido, que o destino continua sob a raiz. Sem isso, um id malformado poderia
// apontar para fora de trabalho/ (ex.: para finalizados/ ou para a casa do usuário).
func caminhoSeguro(raiz, id string) (string, error) {
	if id == "" || id == "." || id == ".." {
		return "", fmt.Errorf("id de pedido inválido: %q", id)
	}
	if strings.ContainsAny(id, `/\`) || filepath.IsAbs(id) {
		return "", fmt.Errorf("id de pedido não pode conter caminho: %q", id)
	}
	if id != filepath.Base(id) {
		return "", fmt.Errorf("id de pedido inválido: %q", id)
	}
	raizAbs, err := filepath.Abs(raiz)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(raizAbs, id)
	// Confirma que o resultado está mesmo sob a raiz (defesa dupla).
	if dir != raizAbs && !strings.HasPrefix(dir, raizAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("caminho fora da raiz de trabalho: %q", dir)
	}
	return dir, nil
}
