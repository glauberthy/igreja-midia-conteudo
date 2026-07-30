package videocache

// Expiração do cache (spec-05 v3, Parte 3; revisa a spec-06).
//
// # Por que prazo E teto, avaliados juntos
//
// Prazo sozinho não protege o disco: uma semana movimentada (cinco cultos de ~570 MB em três
// dias) enche 2,8 GB antes de qualquer coisa completar 30 dias. Teto sozinho não limpa: um
// cache pequeno guardaria para sempre um culto de março que ninguém vai reprocessar. As duas
// réguas medem coisas diferentes — "não vai mais servir" e "não cabe" — e por isso são
// avaliadas no MESMO passe, do mais antigo para o mais novo.
//
// # Idade por último USO, não por download
//
// A conta é sobre usado_em (Cache.Tocar), não baixado_em. Um culto reprocessado toda semana tem
// download antigo e uso recente; FIFO puro apagaria justamente o vídeo mais útil do cache.
//
// # O que expira de dentro da pasta do culto
//
// NÃO é a pasta inteira: são os arquivos que retencao.PodeRemover já classifica como material
// bruto regenerável. A lista é a MESMA da limpeza por pedido, e os nomes dos arquivos também
// são (video.mp4, legenda.srt, legenda.info.json, transcricao.txt) — então reusar aquela
// decisão dá exatamente o veredito desejado aqui, sem uma segunda lista para divergir:
//
//	video.mp4          ~570 MB  removível  → é toda a pressão de disco
//	legenda.info.json     ~4 KB removível  → metadado do yt-dlp
//	legenda.srt         ~281 KB preservado → 0,03% do peso, e custa 3 s + uma requisição ao
//	                                         YouTube para recuperar
//	transcricao.txt     ~130 KB preservado → insumo de auditoria, derivado da legenda
//	video.json           ~200 B  (fora das duas listas → não removível)
//
// Apagar a legenda aqui recriaria, um nível acima, a contradição que a spec-06 tinha: a limpeza
// por pedido apagava legenda.srt com o comentário "baixa de novo", escrito quando não havia
// cache. Corrigido em 2026-07-29; não vale reintroduzir do lado de fora.
//
// O resíduo preservado é ~400 KB por culto expirado, e cresce para sempre. Dito em número para
// não ser descoberto depois: ~21 MB por ano a um culto por semana.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"srtclean/internal/retencao"
)

// DiasPadrao e TetoPadrao são a política escolhida pelo dono (30 dias, 50 GB) — mais folgada que
// os 14 dias / 20 GB que o desenho propôs. 50 GB são ~88 cultos de 570 MB, e o disco tinha
// 516 GB livres na medição.
const (
	DiasPadrao       = 30
	TetoPadrao int64 = 50 << 30
)

// Motivos da expiração, para o relatório dizer QUAL régua pegou o culto.
const (
	MotivoPrazo = "prazo"
	MotivoTeto  = "teto"
)

// OpcoesExpiracao parametriza a expiração.
//
// Intocaveis tem o mesmo nome e a mesma semântica de retencao.Opcoes.Intocaveis de propósito: é
// o MESMO mecanismo de "não toca no que está em uso", só que a unidade aqui é o id do VÍDEO e
// não o id do pedido — um vídeo do cache serve vários pedidos. Quem monta a lista é o servidor,
// no mesmo passe e sob o mesmo mutex que monta a dos pedidos (ver emCursoLocked).
type OpcoesExpiracao struct {
	Dias       int      // idade máxima desde o último uso; 0 usa DiasPadrao
	Teto       int64    // tamanho máximo do cache em bytes; 0 usa TetoPadrao
	Intocaveis []string // ids de VÍDEO que não podem ser tocados (pedidos em curso)
	DryRun     bool
}

// CultoExpirado descreve o que foi (ou seria) removido de um culto.
type CultoExpirado struct {
	VideoID  string
	Motivo   string // MotivoPrazo ou MotivoTeto
	Arquivos []string
	Bytes    int64
	UsadoEm  time.Time
}

// ResultadoExpiracao agrega a passagem.
type ResultadoExpiracao struct {
	Cultos         []CultoExpirado
	BytesLiberados int64
	Retidos        []string // dentro do prazo e caberam no teto
	EmUso          []string // pulados porque um pedido em curso depende deles
	BytesFinais    int64    // tamanho do cache depois (ou depois do que seria removido)
	AcimaDoTeto    bool     // ficou acima do teto mesmo assim (só acontece com vídeo em uso)
	DryRun         bool
}

// Resumo é a linha de log para o operador.
func (r ResultadoExpiracao) Resumo() string {
	verbo := "liberados"
	if r.DryRun {
		verbo = "seriam liberados (dry-run)"
	}
	s := fmt.Sprintf("cache de vídeos: %d culto(s) expirado(s), %s %s; %s em cache, %d retido(s)",
		len(r.Cultos), retencao.FormatarBytes(r.BytesLiberados), verbo,
		retencao.FormatarBytes(r.BytesFinais), len(r.Retidos))
	if len(r.EmUso) > 0 {
		s += fmt.Sprintf("; %d em uso (intocável)", len(r.EmUso))
	}
	if r.AcimaDoTeto {
		s += " — AINDA acima do teto: o que sobra está em uso"
	}
	return s
}

// culto é o estado de uma pasta videos/<id>/ para a decisão.
type culto struct {
	id      string
	usadoEm time.Time
	bytes   int64 // tudo que está na pasta
	// removiveis são os arquivos que a política pode apagar, com os bytes deles. Um culto já
	// expirado tem esta lista vazia — e é o que torna a expiração idempotente sem carimbo novo.
	removiveis []string
}

// Expirar aplica a política e devolve o que foi (ou seria) removido. Idempotente: rodar de novo
// não relista o que já foi expirado.
func (c *Cache) Expirar(o OpcoesExpiracao) (ResultadoExpiracao, error) {
	res := ResultadoExpiracao{DryRun: o.DryRun}
	dias := o.Dias
	if dias <= 0 {
		dias = DiasPadrao
	}
	teto := o.Teto
	if teto <= 0 {
		teto = TetoPadrao
	}

	cultos, err := c.listarCultos()
	if err != nil {
		return res, err
	}
	emUso := map[string]bool{}
	for _, id := range o.Intocaveis {
		if id != "" {
			emUso[id] = true
		}
	}

	var total int64
	for _, cu := range cultos {
		total += cu.bytes
	}
	limite := c.agora().Add(-time.Duration(dias) * 24 * time.Hour)

	// UM passe, do mais antigo para o mais novo, com as duas réguas dentro. Não são duas
	// varreduras: "expirou" é prazo OU (ainda acima do teto), e o total cai conforme se remove,
	// então o teto para de pegar assim que o cache cabe.
	for _, cu := range cultos {
		if emUso[cu.id] {
			res.EmUso = append(res.EmUso, cu.id)
			continue
		}
		if len(cu.removiveis) == 0 {
			continue // já expirado: nada a fazer, e não polui o relatório
		}
		motivo := ""
		switch {
		case cu.usadoEm.Before(limite):
			motivo = MotivoPrazo
		case total > teto:
			motivo = MotivoTeto
		}
		if motivo == "" {
			res.Retidos = append(res.Retidos, cu.id)
			continue
		}
		exp, err := c.removerDoCulto(cu, motivo, o.DryRun)
		if err != nil {
			return res, err
		}
		res.Cultos = append(res.Cultos, exp)
		res.BytesLiberados += exp.Bytes
		total -= exp.Bytes
	}

	res.BytesFinais = total
	res.AcimaDoTeto = total > teto
	return res, nil
}

// listarCultos lê videos/ e devolve os cultos ORDENADOS do último uso mais antigo para o mais
// recente — a ordem em que a política decide.
func (c *Cache) listarCultos() ([]culto, error) {
	entradas, err := os.ReadDir(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // cache nunca usado: nada a expirar
		}
		return nil, fmt.Errorf("lendo o cache em %s: %w", c.Dir, err)
	}
	var cultos []culto
	for _, e := range entradas {
		if !e.IsDir() {
			continue
		}
		if err := idSeguro(e.Name()); err != nil {
			continue // pasta estranha no cache: ignorada, nunca apagada
		}
		cultos = append(cultos, c.medirCulto(e.Name()))
	}
	sort.Slice(cultos, func(a, b int) bool {
		if !cultos[a].usadoEm.Equal(cultos[b].usadoEm) {
			return cultos[a].usadoEm.Before(cultos[b].usadoEm)
		}
		return cultos[a].id < cultos[b].id // desempate estável
	})
	return cultos, nil
}

// medirCulto soma os bytes da pasta e resolve o último uso.
func (c *Cache) medirCulto(videoID string) culto {
	cu := culto{id: videoID}
	dir := filepath.Join(c.Dir, videoID)
	arqs, err := os.ReadDir(dir)
	if err != nil {
		return cu
	}
	var maisRecente time.Time
	for _, a := range arqs {
		fi, err := a.Info()
		if err != nil || a.IsDir() {
			continue
		}
		cu.bytes += fi.Size()
		if m := fi.ModTime(); m.After(maisRecente) {
			maisRecente = m
		}
		if retencao.PodeRemover(a.Name()) {
			cu.removiveis = append(cu.removiveis, a.Name())
		}
	}
	// usado_em vem do video.json. Se ele faltar ou vier zerado (cache preenchido à mão, ou
	// migração antiga), cai para o mtime mais recente da pasta: dado pior, mas nunca "idade
	// zero" — que faria um culto velho parecer novinho e nunca expirar.
	if idx, err := c.LerIndice(videoID); err == nil && !idx.UsadoEm.IsZero() {
		cu.usadoEm = idx.UsadoEm
	} else {
		cu.usadoEm = maisRecente
	}
	return cu
}

// removerDoCulto apaga os arquivos removíveis de um culto. Os nomes vêm do medirCulto (já
// filtrados por retencao.PodeRemover) e o caminho é remontado sob c.Dir com idSeguro — nenhuma
// remoção usa caminho vindo de fora.
func (c *Cache) removerDoCulto(cu culto, motivo string, dryRun bool) (CultoExpirado, error) {
	exp := CultoExpirado{VideoID: cu.id, Motivo: motivo, UsadoEm: cu.usadoEm}
	dir, err := c.DirVideo(cu.id)
	if err != nil {
		return exp, err
	}
	for _, nome := range cu.removiveis {
		alvo := filepath.Join(dir, nome)
		fi, err := os.Stat(alvo)
		if err != nil {
			continue
		}
		if !dryRun {
			if err := os.Remove(alvo); err != nil {
				return exp, fmt.Errorf("expirando %s: %w", alvo, err)
			}
		}
		exp.Arquivos = append(exp.Arquivos, nome)
		exp.Bytes += fi.Size()
	}
	return exp, nil
}
