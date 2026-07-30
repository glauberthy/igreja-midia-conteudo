// Pacote videocache guarda o material que é do CULTO, não do pedido: o vídeo, a legenda e a
// transcrição íntegra. Um culto baixado uma vez serve qualquer janela e qualquer pedido.
//
// # Por que dois níveis de armazenamento
//
//	videos/<videoID>/     imutável, reutilizável, PESADO (~570 MB) — é deste pacote
//	  video.mp4
//	  video.json          {video_id, origem_ms, baixado_em, usado_em, bytes, titulo}
//	  legenda.srt
//	  legenda.info.json
//	  transcricao.txt     do vídeo INTEIRO
//
//	trabalho/<pedidoID>/  depende da janela e das decisões; leve (KB) — não é deste pacote
//	  pedido.json         (com video_id e a proveniência do recorte)
//	  transcricao.txt     RECORTADA à janela (DERIVADA — ver DerivarTranscricao)
//	  candidatos.corrigido.json
//
// Antes disto, cada pedido criava trabalho/<id>/ e rebaixava tudo — inclusive os 570 MB de
// vídeo do mesmo culto de meia hora antes.
//
// # A origem mora AO LADO do vídeo
//
// video.json descreve videos/<id>/video.mp4. pedido.json.origem_ms descreve
// trabalho/<id>/video.mp4 (fluxo do cmd/baixar por janela, que continua existindo). São dois
// arquivos e duas declarações, e é por isso que existe UM resolvedor: Localizar. Ler a origem
// por fora dele é a classe de bug que já produziu Shorts da cena errada com a duração certa
// (spec-09) — há um teste que varre o código e falha se alguém contornar.
package videocache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/transcricao"
)

// Nomes dos arquivos dentro de videos/<videoID>/. Em um lugar só: quem escreve e quem lê
// referenciam a mesma constante, senão o cache "não acerta" por diferença de nome.
const (
	NomeVideo   = "video.mp4"
	NomeIndice  = "video.json"
	NomeLegenda = "legenda.srt"
	NomeInfo    = "legenda.info.json"
	NomeTransc  = "transcricao.txt"
)

// DirPadrao é a raiz do cache. Fora de trabalho/ de propósito: a limpeza por pedido
// (spec-06) enxerga só a raiz de trabalho, então o cache não pode morar lá dentro — seria
// apagado pela política errada.
const DirPadrao = "videos"

// OrigemVideoInteiro é a origem de tempo de um arquivo que contém o vídeo INTEIRO: zero, por
// definição — o t=0 do arquivo é o t=0 do vídeo do YouTube. Nomeado porque zero aqui é uma
// AFIRMAÇÃO ("começa no começo"), não um default.
const OrigemVideoInteiro = 0

// ErrSemVideo diz que não há vídeo nem no pedido nem no cache. É erro, nunca um padrão
// silencioso: sem saber a que instante o arquivo corresponde, um corte deslocado sai com a
// duração certa e a cena errada.
var ErrSemVideo = errors.New("nenhum vídeo disponível para este pedido")

// Indice é o video.json: o que descreve o arquivo de vídeo do cache.
type Indice struct {
	VideoID   string    `json:"video_id"`
	OrigemMs  int       `json:"origem_ms"`  // instante absoluto correspondente ao t=0 do arquivo
	BaixadoEm time.Time `json:"baixado_em"` // quando entrou no cache
	UsadoEm   time.Time `json:"usado_em"`   // última vez que um pedido o aproveitou (LRU)
	Bytes     int64     `json:"bytes"`
	Titulo    string    `json:"titulo,omitempty"`
}

// Cache aponta para a raiz do cache. Agora é injetável para os testes decidirem o tempo.
type Cache struct {
	Dir   string
	Agora func() time.Time
	// MinBytes é o tamanho mínimo para um arquivo contar como vídeo utilizável. 0 usa
	// MinBytesVideo (20 MB), que é o valor de produção.
	//
	// Injetável só por causa dos testes que geram vídeo sintético de verdade (o de conteúdo
	// de frame gera 120 s em 192x108, uns 300 KB). Sem isto, aquele teste teria de encher 20 MB
	// de bytes inúteis, ou a regra teria de ser afrouxada em produção — e o limiar existe
	// justamente para um `.part` de 8 MB não ser tratado como vídeo.
	MinBytes int64
}

func (c *Cache) minBytes() int64 {
	if c.MinBytes > 0 {
		return c.MinBytes
	}
	return MinBytesVideo
}

// Usavel diz se o caminho aponta para um vídeo utilizável por ESTE cache (respeitando um
// MinBytes injetado). É a regra única — quem precisa decidir "isto é vídeo?" chama aqui.
func (c *Cache) Usavel(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() >= c.minBytes()
}

// Novo cria um Cache na raiz dada ("" usa DirPadrao).
func Novo(dir string) *Cache {
	if dir == "" {
		dir = DirPadrao
	}
	return &Cache{Dir: dir}
}

func (c *Cache) agora() time.Time {
	if c.Agora != nil {
		return c.Agora()
	}
	return time.Now()
}

// DirVideo é videos/<videoID>. Devolve erro se o id não servir como nome de diretório — a
// validação de formato é do download.VideoID, e aqui é a segunda linha de defesa: nenhum
// caminho é montado a partir de id vazio ou com separador.
func (c *Cache) DirVideo(videoID string) (string, error) {
	if err := idSeguro(videoID); err != nil {
		return "", err
	}
	return filepath.Join(c.Dir, videoID), nil
}

func idSeguro(videoID string) error {
	if videoID == "" {
		return fmt.Errorf("id de vídeo vazio")
	}
	if videoID != filepath.Base(videoID) || videoID == "." || videoID == ".." {
		return fmt.Errorf("id de vídeo inválido como nome de pasta: %q", videoID)
	}
	return nil
}

// MinBytesVideo é o tamanho mínimo para um arquivo ser considerado vídeo de culto utilizável.
// Abaixo disto é resto de download interrompido (`.part` renomeado, merge incompleto), e
// tratá-lo como vídeo produziria um Short vazio ou um erro obscuro do ffmpeg.
//
// EXPORTADA porque a pergunta "este arquivo é um vídeo usável?" tem de ter UMA resposta no
// sistema. O servidor tinha a sua (20 MB) e o cache nasceu com outra (1 MB) — dois limiares
// para a mesma pergunta, que é a duplicação de regra que este projeto já pagou caro. Quem
// precisa decidir isso usa esta constante.
//
// 20 MB: um culto de 35 min em 720p passa de 100 MB; um parcial já observado tinha 8 MB.
const MinBytesVideo = 20 << 20

// TemVideo diz se o vídeo do culto já está em disco e utilizável.
func (c *Cache) TemVideo(videoID string) bool {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return false
	}
	return c.Usavel(filepath.Join(dir, NomeVideo))
}

// TemLegenda diz se a LEGENDA do culto já está no cache.
//
// Pergunta só pela legenda.srt, que é a FONTE. A transcrição íntegra é derivada dela e é
// regenerada quando falta (GarantirTranscricaoIntegra) — antes esta função exigia as duas, e
// o efeito era baixar 3 s de legenda de novo só porque um arquivo derivado tinha sumido.
//
// Não existe uma pergunta "o cache está completo": existem duas perguntas independentes,
// TemVideo e TemLegenda, cada uma abrindo o download do que falta. É o que faz um cache
// migrado (vídeo movido, legenda ainda não) baixar só a legenda e não tocar nos 820 MB.
func (c *Cache) TemLegenda(videoID string) bool {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, NomeLegenda))
	return err == nil && fi.Size() > 0
}

// GarantirTranscricaoIntegra gera a transcrição íntegra se ela não estiver lá. É derivada da
// legenda, determinística e barata — então "faltando" se resolve regenerando, nunca baixando.
func (c *Cache) GarantirTranscricaoIntegra(videoID string) error {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(filepath.Join(dir, NomeTransc)); err == nil && fi.Size() > 0 {
		return nil
	}
	return c.GerarTranscricaoIntegra(videoID)
}

// LerIndice lê o video.json do culto.
func (c *Cache) LerIndice(videoID string) (Indice, error) {
	var idx Indice
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return idx, err
	}
	b, err := os.ReadFile(filepath.Join(dir, NomeIndice))
	if err != nil {
		return idx, err
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return idx, fmt.Errorf("video.json de %s ilegível: %w", videoID, err)
	}
	return idx, nil
}

// ErrOrigemNaoZero recusa entrada no cache de vídeo que não seja o culto INTEIRO.
var ErrOrigemNaoZero = errors.New("o cache só aceita o vídeo INTEIRO (origem 0)")

// Aceita diz se um vídeo com essa origem pode entrar no cache. É a REGRA, em um lugar só.
//
// Existe como função separada do Registrar porque há um chamador que precisa perguntar ANTES
// de agir: a migração (internal/servidor/migrarcache.go) move o arquivo e só então registra;
// se descobrisse a recusa depois do `rename`, teria movido o vídeo do pedido para o cache e o
// deixado lá sem video.json — pior que não ter migrado.
//
// Duas checagens, mesma regra, um lugar. Sem isto seriam duas comparações a `OrigemVideoInteiro`
// em pacotes diferentes, que é a forma como regra duplicada diverge neste projeto.
func Aceita(origemMs int) error {
	if origemMs != OrigemVideoInteiro {
		return fmt.Errorf("%w: origem %d ms. Um arquivo de JANELA no cache declararia origem 0 e "+
			"começaria %d ms adiante — a origem mentiria sobre o conteúdo, EM DISCO, para todo "+
			"pedido que reusasse este culto", ErrOrigemNaoZero, origemMs, origemMs)
	}
	return nil
}

// Registrar grava o video.json depois de o vídeo entrar no cache. A origem vem de QUEM
// ESCREVEU o arquivo (o baixador devolve), não de uma suposição deste pacote.
//
// # A INVARIANTE DO CACHE: aqui só entra vídeo inteiro
//
// Registrar RECUSA origem != 0. É o que transforma "o cache só contém vídeo inteiro" de
// convenção em invariante do pacote, verificada num lugar só e independente de quem chama.
//
// Por que isso é mais grave que o bug que já pagamos: um vídeo baixado por JANELA
// (cmd/baixar, origem = início da pregação) registrado no cache viraria um arquivo cuja
// origem DECLARADA mente sobre o conteúdo — 0 dizendo "começa no começo" num arquivo que
// começa 49 min adiante. O bug de corte deslocado ficava na memória de uma execução; este
// ficaria GRAVADO EM DISCO, e envenenaria todo pedido futuro que reusasse aquele culto,
// inclusive de outro sermão. Errado que persiste e se espalha é de outra categoria.
//
// A migração (internal/servidor/migrarcache.go) já checava isso antes de mover o arquivo, e
// era a checagem certa no lugar errado: protegia UM caminho. Qualquer via nova de escrita
// reabriria o furo. Agora o guarda é do pacote — quem escreve no cache passa por aqui.
func (c *Cache) Registrar(videoID string, origemMs int, titulo string) error {
	if err := Aceita(origemMs); err != nil {
		return fmt.Errorf("recusando registrar o vídeo %s: %w", videoID, err)
	}
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return err
	}
	var bytes int64
	if fi, err := os.Stat(filepath.Join(dir, NomeVideo)); err == nil {
		bytes = fi.Size()
	}
	agora := c.agora()
	idx := Indice{
		VideoID: videoID, OrigemMs: origemMs,
		BaixadoEm: agora, UsadoEm: agora, Bytes: bytes, Titulo: titulo,
	}
	return c.gravarIndice(dir, idx)
}

// Tocar atualiza o usado_em (LRU). A expiração conta a idade pelo último USO, não pelo
// download: um culto reprocessado toda semana tem download antigo e uso recente, e FIFO puro
// apagaria justamente o vídeo mais útil.
//
// Falha de I/O aqui é AVISO, não erro do pedido: o pior efeito é o vídeo expirar mais cedo.
func (c *Cache) Tocar(videoID string) error {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return err
	}
	idx, err := c.LerIndice(videoID)
	if err != nil {
		return err
	}
	idx.UsadoEm = c.agora()
	return c.gravarIndice(dir, idx)
}

func (c *Cache) gravarIndice(dir string, idx Indice) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, NomeIndice), b, 0644)
}

// Fonte é o par indissociável que o render precisa: o ARQUIVO e a que instante do vídeo
// original o t=0 dele corresponde. Andam juntos porque um sem o outro produz corte deslocado
// silencioso — foi o bug do Short 49 min fora de lugar (spec-09).
type Fonte struct {
	Path     string
	OrigemMs int
	// DoCache diz de onde a fonte veio (para log e para os testes serem específicos).
	DoCache bool
}

// Localizar é o ÚNICO lugar do sistema que resolve "qual arquivo de vídeo e qual origem".
//
// Precedência, explícita: o vídeo na pasta do PEDIDO vence (é o fluxo do cmd/baixar por
// janela, mais específico e possivelmente recortado); senão o do CACHE; se nenhum dos dois,
// ErrSemVideo com o que falta.
//
// Nunca deduz. Nem por ped.Inicio (era a suposição que produziu o bug), nem pela duração do
// arquivo (uma janela de 35 min e um vídeo inteiro de 35 min são indistinguíveis).
//
// Quem renderiza chama isto e passa a Fonte adiante. Um teste varre o código e falha se algum
// consumidor de vídeo ler a origem por fora daqui (videocache/resolvedor_unico_test.go).
func (c *Cache) Localizar(baseDir string, ped *pipeline.Pedido) (Fonte, error) {
	noPedido := filepath.Join(baseDir, ped.ID, NomeVideo)
	if c.Usavel(noPedido) {
		origem, err := ped.Origem()
		if err != nil {
			return Fonte{}, err // a mensagem do Pedido já diz o que fazer
		}
		return Fonte{Path: noPedido, OrigemMs: origem}, nil
	}

	if ped.VideoID == "" {
		return Fonte{}, fmt.Errorf("%w: não há vídeo em %s e o pedido não diz de qual vídeo do "+
			"YouTube ele é (campo video_id ausente em pedido.json)", ErrSemVideo, noPedido)
	}
	if !c.TemVideo(ped.VideoID) {
		dir, _ := c.DirVideo(ped.VideoID)
		return Fonte{}, fmt.Errorf("%w: não há vídeo em %s nem em %s. Rode a fase pesada (ou o "+
			"cmd/baixar) para o vídeo entrar em disco", ErrSemVideo, noPedido,
			filepath.Join(dir, NomeVideo))
	}
	idx, err := c.LerIndice(ped.VideoID)
	if err != nil {
		dir, _ := c.DirVideo(ped.VideoID)
		return Fonte{}, fmt.Errorf("o vídeo %s existe no cache mas %s não declara a origem de "+
			"tempo dele (%v). Sem isso, um corte deslocado sai com a duração certa e a cena "+
			"errada: acrescente {\"video_id\":%q,\"origem_ms\":0} ou rebaixe o vídeo",
			ped.VideoID, filepath.Join(dir, NomeIndice), err, ped.VideoID)
	}
	dir, err := c.DirVideo(ped.VideoID)
	if err != nil {
		return Fonte{}, err
	}
	return Fonte{Path: filepath.Join(dir, NomeVideo), OrigemMs: idx.OrigemMs, DoCache: true}, nil
}

// JanelaInteira é o fim de janela que significa "até o fim do vídeo". Usado para gerar a
// transcrição ÍNTEGRA do cache com a MESMA função que gera a recortada do pedido — a íntegra
// não é um terceiro dado, é a mesma derivação com a janela toda.
const JanelaInteira = 1 << 40 // ms; ~34 anos, qualquer culto cabe

// DerivarTranscricao gera uma transcrição limpa recortada à janela [inicioMs, fimMs) a partir
// da LEGENDA do cache (videos/<id>/legenda.srt), grava em `destino` e devolve a proveniência
// para quem chama registrar no pedido.json.
//
// A fonte é a legenda .srt, não a transcrição íntegra: é assim que o recorte sai IDÊNTICO ao
// que o pipeline sempre produziu (o srtclean desduplica considerando os blocos da janela; um
// recorte por timestamp sobre o texto já limpo daria fronteira diferente). A íntegra do cache
// é a mesma chamada com JanelaInteira — mesma função, janela maior.
//
// Por que a derivada existe, em vez de todos lerem a íntegra e recortarem na hora: a seleção,
// a faixa de frases da revisão e o cmd/auditar leem transcricao.txt POR CAMINHO DE ARQUIVO.
// Mudar os três para resolver "vídeo + janela" é custo sem retorno. Então a derivada fica, com
// duas regras que a tornam segura:
//
//  1. o pedido declara de onde ela veio (pipeline.Pedido.Recorte);
//  2. ela NUNCA é editada — ou é regenerada, ou está errada.
//
// A função é determinística: mesma legenda e mesma janela produzem os MESMOS BYTES. É o que o
// teste de regeneração verifica, e é o que acusaria uma íntegra que mudou por rebaixamento do
// vídeo.
func (c *Cache) DerivarTranscricao(videoID, destino string, inicioMs, fimMs int) (pipeline.Recorte, error) {
	var rec pipeline.Recorte
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return rec, err
	}
	legenda := filepath.Join(dir, NomeLegenda)
	if _, err := os.Stat(legenda); err != nil {
		return rec, fmt.Errorf("legenda do vídeo %s ausente em %s: %w", videoID, legenda, err)
	}
	if err := os.MkdirAll(filepath.Dir(destino), 0755); err != nil {
		return rec, err
	}
	if _, _, err := transcricao.LimparArquivoJanela(legenda, destino, inicioMs, fimMs); err != nil {
		return rec, fmt.Errorf("recortando a transcrição à janela: %w", err)
	}
	return pipeline.Recorte{
		VideoID: videoID,
		Inicio:  transcricao.FormatMs(inicioMs),
		Fim:     transcricao.FormatMs(fimMs),
	}, nil
}

// GerarTranscricaoIntegra escreve videos/<id>/transcricao.txt: a transcrição do vídeo INTEIRO.
// É a mesma derivação de DerivarTranscricao com a janela toda — de propósito, para a íntegra
// não ser um dado com regra própria.
func (c *Cache) GerarTranscricaoIntegra(videoID string) error {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return err
	}
	_, err = c.DerivarTranscricao(videoID, filepath.Join(dir, NomeTransc), 0, JanelaInteira)
	return err
}

// ===== PAUSAS DE FALA (fronteiras vindas do áudio) =====
//
// Mora no cache porque é do CULTO, não do pedido: uma passada de silencedetect (6,5 s medidos
// para 1h50) serve toda janela e todo pedido daquele vídeo, hoje e na semana que vem — a mesma
// razão do vídeo, da legenda e da transcrição íntegra.
//
// O arquivo guarda os PARÂMETROS usados junto das pausas. Sem isso, mudar o limiar deixaria em
// disco um resultado cuja origem ninguém sabe — e a régua desenhada com um limiar discordaria do
// encaixe calculado com outro, que é a forma de o operador perder confiança nos dois.

// NomePausas é o arquivo das pausas dentro de videos/<videoID>/.
const NomePausas = "pausas.json"

// Pausa é um intervalo de silêncio, em ms absolutos do vídeo.
//
// Sim, o internal/video tem uma struct igual — e é de propósito. Este pacote é ARMAZENAMENTO e
// não pode importar o que roda ffmpeg: o internal/video importa videocache no teste de conteúdo
// de frame, então a dependência inversa fecha um ciclo. Mais fundo que o ciclo: dar ao
// internal/video acesso ao videocache devolveria a ele o caminho para descobrir a origem do
// vídeo, que a spec-09 fechou tornando IMPOSSÍVEL, não proibido.
//
// O preço são duas structs de dois campos e uma conversão de três linhas, num lugar só
// (servidor.garantirPausas). Barato, e não é "duas listas para o mesmo dado": uma é o resultado
// do ffmpeg, a outra é o formato do arquivo — e quem grava converte na fronteira.
type Pausa struct {
	InicioMs int `json:"inicio_ms"` // instante em que a fala PAROU
	FimMs    int `json:"fim_ms"`    // instante em que a fala VOLTOU
}

// DuracaoMs é o tamanho da pausa — pista de ordenação (pausa longa é mais provável fim de
// sentença), nunca classificador: a distribuição medida é unimodal, e há pausa de 467 ms que é
// fim de frase e de 934 ms que é meio de frase. Ver internal/video/pausas.go.
func (p Pausa) DuracaoMs() int { return p.FimMs - p.InicioMs }

// AnalisePausas é o pausas.json: as pausas e a receita que as produziu.
type AnalisePausas struct {
	NoiseDB  int       `json:"noise_db"`
	MinMs    int       `json:"min_ms"`
	GeradoEm time.Time `json:"gerado_em"`
	Pausas   []Pausa   `json:"pausas"`
}

// Compativel diz se esta análise foi feita com os parâmetros pedidos. Parâmetro diferente =
// análise a refazer: 6,5 s é barato demais para justificar usar dado de receita desconhecida.
func (a AnalisePausas) Compativel(noiseDB, minMs int) bool {
	return a.NoiseDB == noiseDB && a.MinMs == minMs
}

// GravarPausas escreve o pausas.json do culto.
func (c *Cache) GravarPausas(videoID string, a AnalisePausas) error {
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if a.GeradoEm.IsZero() {
		a.GeradoEm = c.agora()
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, NomePausas), b, 0644)
}

// LerPausas devolve a análise gravada. Erro se não existir — quem chama decide o que fazer
// (hoje: cair na fronteira da legenda, dizendo qual regra usou).
func (c *Cache) LerPausas(videoID string) (AnalisePausas, error) {
	var a AnalisePausas
	dir, err := c.DirVideo(videoID)
	if err != nil {
		return a, err
	}
	b, err := os.ReadFile(filepath.Join(dir, NomePausas))
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return a, fmt.Errorf("pausas.json de %s ilegível: %w", videoID, err)
	}
	return a, nil
}
