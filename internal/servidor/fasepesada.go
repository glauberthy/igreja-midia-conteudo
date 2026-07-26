package servidor

import (
	"srtclean/internal/validacao"
)

// candidatosAprovados devolve os candidatos que o operador aprovou (reg.aprovados são os
// índices ORIGINAIS). Deve ser chamada com o lock do servidor seguro.
func candidatosAprovados(reg *registro) []validacao.Candidato {
	var out []validacao.Candidato
	for _, idx := range reg.aprovados {
		if idx >= 0 && idx < len(reg.cands) {
			out = append(out, reg.cands[idx])
		}
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
