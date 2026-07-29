package servidor

import (
	"fmt"

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

// origemVideoCompleto é a origem de tempo quando o arquivo baixado é o VÍDEO INTEIRO: o
// t=0 do arquivo é o t=0 do vídeo, então não há deslocamento nenhum e o render corta em
// tempo ABSOLUTO (start/end do candidato, como vieram da seleção).
//
// É o contrato mais simples possível — e essa simplicidade é metade do motivo de baixar o
// vídeo inteiro (a outra metade é velocidade: 7,3 s contra 577 s da janela contígua). Com a
// janela contígua havia uma origem calculada (menor start, piso ao segundo) que precisava
// ser propagada corretamente do download até o render; qualquer descasamento aí produzia
// Short do trecho errado. Com origem 0, esse cálculo — e a classe de bug — deixam de existir.
const origemVideoCompleto = 0

// declararOrigemVideoInteiro registra no pedido — e em disco — que o video.mp4 baixado é o
// vídeo INTEIRO, ou seja, que o t=0 do arquivo é o t=0 do vídeo do YouTube.
//
// Por que gravar em disco: o pedido.json é o que o cmd/render, o cmd/auditar e a retomada
// leem depois. Sem a declaração persistida, o `cmd/render -id <pedido do servidor>` volta a
// ser um comando que não tem como saber a origem do arquivo que ele mesmo vai cortar.
//
// Falha de I/O aqui é AVISO, não erro do pedido: o render desta execução recebe a origem em
// memória e continua. O que fica prejudicado é só o uso posterior pela CLI.
func (s *Servidor) declararOrigemVideoInteiro(reg *registro) {
	s.mu.Lock()
	reg.ped.DeclararOrigem(origemVideoCompleto)
	copia := *reg.ped
	s.mu.Unlock()
	if err := copia.Salvar(s.baseDir); err != nil {
		s.logTempos(fmt.Sprintf("aviso: não gravei origem_ms=%d no pedido.json de %s: %v "+
			"(o render desta execução não é afetado; o cmd/render depois vai reclamar)",
			origemVideoCompleto, copia.ID, err))
	}
}
