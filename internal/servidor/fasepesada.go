package servidor

import (
	"fmt"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// candidatosAprovados devolve os candidatos que o operador aprovou (reg.aprovados são os
// índices ORIGINAIS). Deve ser chamada com o lock do servidor seguro.
func candidatosAprovados(reg *registro) []validacao.Candidato {
	var out []validacao.Candidato
	for _, idx := range reg.aprovados {
		if idx < 0 || idx >= len(reg.cands) {
			continue
		}
		c := reg.cands[idx]
		// Se o operador refez o corte à mão (spec-05 v2), são os tempos DELE que valem —
		// já recalculados e validados no /aprovar. O render recebe o ajuste, não o
		// original: é o ponto inteiro da funcionalidade.
		if t, ok := reg.ajustes[idx]; ok {
			c = aplicarAjuste(c, t)
		}
		out = append(out, c)
	}
	return out
}

// NOTA HISTÓRICA — aqui existia a constante origemVideoCompleto = 0, e a fase pesada a
// passava direto ao render. Saiu porque era o servidor AFIRMANDO um fato do baixador: dois
// lugares dizendo "vídeo inteiro → origem 0", e dois lugares que afirmam a mesma coisa
// divergem. Agora o baixador DEVOLVE a origem do arquivo que escreveu e a fase pesada só
// guarda (registrarOrigem) e repassa.
//
// O comentário que justificava o zero continua valendo e mora no lugar certo:
// download.BaixarVideoCompleto e download.origemVideoInteiro.
//
// Vale registrar por que o valor é simples: baixar o vídeo inteiro foi escolhido metade por
// velocidade (7,3 s contra 577 s da janela contígua) e metade por causa deste contrato. Com a
// janela contígua havia uma origem CALCULADA (menor start, piso ao segundo) que precisava ser
// propagada corretamente do download até o render; qualquer descasamento produzia Short do
// trecho errado. Com o vídeo inteiro, o cálculo desaparece — mas a propagação continuou
// existindo até virar campo declarado, e foi ali que a classe de bug reapareceu.

// registrarRecorte guarda no pedido — e em disco — a PROVENIÊNCIA da transcrição recortada:
// de qual vídeo e de qual janela ela saiu.
//
// Não é burocracia: a transcrição existe em dois lugares (íntegra no cache, recortada no
// pedido), e duas cópias do mesmo dado é como nasceram os dois bugs mais caros deste projeto.
// A diferença aqui é que uma é DERIVÁVEL da outra. Declarar a proveniência é o que permite a
// um teste regenerar o recorte a partir do cache e comparar byte a byte — se o vídeo for
// rebaixado e a legenda mudar, é esse teste que acusa.
func (s *Servidor) registrarRecorte(reg *registro, rec pipeline.Recorte) {
	s.mu.Lock()
	reg.ped.DeclararRecorte(rec.VideoID, rec.Inicio, rec.Fim)
	copia := *reg.ped
	s.mu.Unlock()
	if err := copia.Salvar(s.baseDir); err != nil {
		s.logTempos(fmt.Sprintf("aviso: não gravei a proveniência do recorte de %s: %v", copia.ID, err))
	}
}

// tituloDoCache lê o título gravado no video.json. Serve o acerto de cache: sem baixar o
// .info.json de novo, o pedido ainda mostra o nome do culto na tela e no CSV de tempos.
func (s *Servidor) tituloDoCache(videoID string) string {
	idx, err := s.cache.LerIndice(videoID)
	if err != nil {
		return ""
	}
	return idx.Titulo
}

// registrarOrigem guarda no pedido — e em disco — a origem de tempo que o BAIXADOR devolveu
// para o video.mp4 que ele escreveu. Não decide o valor: só o registra.
//
// Por que gravar em disco: o pedido.json é o que o cmd/render e a retomada leem depois. Sem a
// declaração persistida, o `cmd/render -id <pedido do servidor>` volta a ser um comando sem
// como saber a origem do arquivo que ele mesmo vai cortar.
//
// Falha de I/O aqui é AVISO, não erro do pedido: o render desta execução recebe a origem em
// memória e continua. O que fica prejudicado é só o uso posterior pela CLI.
func (s *Servidor) registrarOrigem(reg *registro, origemMs int) {
	s.mu.Lock()
	reg.ped.DeclararOrigem(origemMs)
	copia := *reg.ped
	s.mu.Unlock()
	if err := copia.Salvar(s.baseDir); err != nil {
		s.logTempos(fmt.Sprintf("aviso: não gravei origem_ms=%d no pedido.json de %s: %v "+
			"(o render desta execução não é afetado; o cmd/render depois vai reclamar)",
			origemMs, copia.ID, err))
	}
}
