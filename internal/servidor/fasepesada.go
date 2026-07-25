package servidor

import (
	"srtclean/internal/transcricao"
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

// janelaDownload calcula a janela CONTÍGUA a baixar para os trechos aprovados: do menor
// start ao maior end. Devolve:
//   - iniHms/fimHms em "HH:MM:SS" (o que o --download-sections do yt-dlp recebe): início
//     no PISO ao segundo do menor start (inclui a abertura); fim no TETO ao segundo do
//     maior end (inclui o fecho);
//   - origemMs = o piso ao segundo do menor start = o instante ABSOLUTO que corresponde ao
//     t=0 do video.mp4 baixado. O render usa esta MESMA origem (RenderizarComOrigem), então
//     cada corte é (start - origemMs) e cai exatamente no trecho pedido.
//
// Piso/teto ao segundo espelham o contrato do CLI (ped.Inicio/Fim são segundos inteiros) e
// garantem que a janela NUNCA aperte os trechos (começa em/antes do 1º start, termina em/
// depois do último end).
func janelaDownload(aprovados []validacao.Candidato) (iniHms, fimHms string, origemMs int) {
	minStart, maxEnd := -1, -1
	for _, c := range aprovados {
		s, okS := transcricao.HmsToMs(c.Start)
		e, okE := transcricao.HmsToMs(c.End)
		if okS && (minStart == -1 || s < minStart) {
			minStart = s
		}
		if okE && e > maxEnd {
			maxEnd = e
		}
	}
	if minStart < 0 {
		minStart = 0
	}
	if maxEnd < minStart {
		maxEnd = minStart
	}
	origemMs = (minStart / 1000) * 1000     // piso ao segundo
	fimMs := ((maxEnd + 999) / 1000) * 1000 // teto ao segundo
	return transcricao.FormatMs(origemMs), transcricao.FormatMs(fimMs), origemMs
}
