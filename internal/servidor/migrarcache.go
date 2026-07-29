package servidor

import (
	"fmt"
	"os"
	"path/filepath"

	"srtclean/internal/pipeline"
	"srtclean/internal/retencao"
	"srtclean/internal/videocache"
)

// Migração do layout antigo (vídeo dentro da pasta do pedido) para o cache por vídeo.
//
// Antes da spec-05 v3, cada pedido baixava o culto para trabalho/<pedidoID>/video.mp4. Quem
// já tem um desses em disco tem um download BOM — pode ser 800 MB e ~35 s de rede. Jogar fora
// para rebaixar o mesmo arquivo seria desperdício gratuito, então na primeira vez que o pedido
// aparece (criação ou retomada) o arquivo é MOVIDO para videos/<videoID>/.
//
// Mover, não copiar: é o mesmo disco, então é instantâneo e não precisa do dobro de espaço.

// migrarVideoParaCache move trabalho/<id>/video.mp4 para o cache, se fizer sentido.
//
// Não faz nada (e não é erro) quando: não há vídeo na pasta do pedido; o video_id não é
// conhecido; o cache já tem o vídeo deste culto; ou o vídeo do pedido NÃO é o vídeo inteiro.
//
// Essa última condição é a que importa e é fácil de errar: o cache guarda vídeo INTEIRO, com
// origem 0. Um vídeo baixado por JANELA (cmd/baixar, origem = início da pregação) migrado para
// o cache viraria um arquivo cuja origem declarada seria 0 e cujo conteúdo começa 49 min
// adiante — exatamente o bug do corte deslocado, agora com o dado errado gravado em disco. Só
// migra o que o pedido declara como origem 0.
func (s *Servidor) migrarVideoParaCache(ped *pipeline.Pedido) {
	if ped.VideoID == "" {
		return
	}
	origem := filepath.Join(s.baseDir, ped.ID, "video.mp4")
	if !s.videoUsavel(origem) {
		return
	}
	if s.cache.TemVideo(ped.VideoID) {
		return // o cache já tem este culto; o do pedido é duplicata e a limpeza cuida dele
	}

	// A origem TEM de estar declarada e ser 0 (vídeo inteiro). Sem declaração, não sabemos o
	// que este arquivo é, e "não sei" nunca vira "assumo que é o inteiro".
	//
	// A pergunta vai ao RESOLVEDOR, não a ped.OrigemMs: é a mesma informação, e ler o campo
	// aqui criaria um segundo lugar interpretando origem — o começo exato da classe de bug que
	// o Localizar existe para fechar. Um teste varre o código e reprova quem contornar.
	fonte, err := s.cache.Localizar(s.baseDir, ped)
	if err != nil {
		s.logTempos(fmt.Sprintf("migração: %s tem vídeo em disco mas não sabemos a que instante "+
			"do vídeo ele corresponde; deixando onde está (%v)", ped.ID, err))
		return
	}
	// Pergunta ao CACHE se ele aceitaria este vídeo, antes de mover. A regra ("só vídeo
	// inteiro") é do pacote videocache e o Registrar a impõe; aqui o mesmo Aceita é consultado
	// mais cedo, porque descobrir a recusa depois do `rename` deixaria o vídeo fora da pasta do
	// pedido e sem video.json no cache — pior que não ter migrado.
	origemMs := fonte.OrigemMs
	if err := videocache.Aceita(origemMs); err != nil {
		s.logTempos(fmt.Sprintf("migração: o vídeo de %s fica onde está — %v", ped.ID, err))
		return
	}

	dirVideo, err := s.cache.DirVideo(ped.VideoID)
	if err != nil {
		s.logTempos(fmt.Sprintf("migração: id de vídeo inválido em %s: %v", ped.ID, err))
		return
	}
	if err := os.MkdirAll(dirVideo, 0755); err != nil {
		s.logTempos(fmt.Sprintf("migração: não criei %s: %v", dirVideo, err))
		return
	}
	destino := filepath.Join(dirVideo, videocache.NomeVideo)
	bytes := tamanhoArquivo(origem)
	if err := os.Rename(origem, destino); err != nil {
		// Falha comum e legítima: cache em outro sistema de arquivos (rename cruza device).
		// Não é erro do pedido — o Localizar continua achando o vídeo na pasta do pedido,
		// porque a precedência é "pedido vence cache". Só não haverá reaproveitamento.
		s.logTempos(fmt.Sprintf("migração: não movi o vídeo de %s para o cache (%v); ele fica "+
			"onde está e continua utilizável", ped.ID, err))
		return
	}
	if err := s.cache.Registrar(ped.VideoID, origemMs, ped.Titulo); err != nil {
		s.logTempos(fmt.Sprintf("migração: movi o vídeo de %s mas não gravei o video.json (%v); "+
			"o próximo render vai reclamar até isso ser resolvido", ped.ID, err))
		return
	}
	// Legenda e transcrição íntegra também servem ao cache, se ainda não estiverem lá. São
	// leves; copiar é aceitável (e a legenda do pedido é a do culto inteiro, é a mesma coisa).
	s.migrarLegendaParaCache(ped, dirVideo)
	s.logTempos(fmt.Sprintf("migração: vídeo de %s (%s) movido para o cache do culto %s",
		ped.ID, retencao.FormatarBytes(bytes), ped.VideoID))
}

// migrarLegendaParaCache copia a legenda.srt do pedido para o cache e gera a transcrição
// íntegra a partir dela — o que evita rebaixar a legenda no próximo pedido do mesmo culto.
func (s *Servidor) migrarLegendaParaCache(ped *pipeline.Pedido, dirVideo string) {
	if s.cache.TemLegenda(ped.VideoID) {
		return
	}
	srt := filepath.Join(s.baseDir, ped.ID, "legenda.srt")
	b, err := os.ReadFile(srt)
	if err != nil {
		return // sem legenda no pedido: o próximo pedido do culto baixa (3 s), sem drama
	}
	if err := os.WriteFile(filepath.Join(dirVideo, videocache.NomeLegenda), b, 0644); err != nil {
		s.logTempos(fmt.Sprintf("migração: não copiei a legenda de %s: %v", ped.ID, err))
		return
	}
	if err := s.cache.GerarTranscricaoIntegra(ped.VideoID); err != nil {
		s.logTempos(fmt.Sprintf("migração: legenda copiada mas não gerei a transcrição íntegra "+
			"do culto %s: %v", ped.VideoID, err))
	}
}
