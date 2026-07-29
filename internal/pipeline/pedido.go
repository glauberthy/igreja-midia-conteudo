// Pacote pipeline modela um "pedido" (uma solicitação de Shorts a partir de um
// culto) e orquestra as etapas que já existem: seleção pelo modelo + correção
// determinística (internal/validacao). Vídeo entra em specs futuras (03/04/05).
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Estado é o ponto do ciclo de vida em que o pedido está.
//
// O servidor web (spec-05) opera em duas fases separadas por aprovação humana:
// fase leve (baixando-legenda → selecionando → validando → aguardando-aprovacao) e,
// após a aprovação do operador, fase pesada (baixando-video → renderizando →
// concluido). Os estados da fase pesada são consumidos nas Partes 2/3 da spec-05.
type Estado string

const (
	EstadoRecebido     Estado = "recebido"
	EstadoSelecionando Estado = "selecionando"
	EstadoValidando    Estado = "validando"
	EstadoConcluido    Estado = "concluido"
	EstadoErro         Estado = "erro"

	// Estados da interface web (spec-05).
	EstadoBaixandoLegenda         Estado = "baixando-legenda"
	EstadoAguardandoAprovacao     Estado = "aguardando-aprovacao"
	EstadoAguardandoProcessamento Estado = "aguardando-processamento"
	EstadoBaixandoVideo           Estado = "baixando-video"
	EstadoRenderizando            Estado = "renderizando"
)

// nomeArquivo é o JSON de metadados do pedido dentro da pasta de trabalho.
const nomeArquivo = "pedido.json"

// Pedido é a unidade de trabalho do pipeline. É serializável em JSON e serve de
// contrato entre as etapas e as specs seguintes.
//
// O pedido NÃO carrega candidatos (spec-09): a fonte única de verdade dos candidatos
// é o arquivo de seleção validado (candidatos.corrigido.json), lido pelo cmd/render.
// Guardar candidatos aqui criava uma segunda fonte que sombreava a validada.
type Pedido struct {
	ID         string    `json:"id"`
	YouTubeURL string    `json:"youtube_url"`
	Titulo     string    `json:"titulo,omitempty"` // título do vídeo (yt-dlp), preenchido no download da legenda
	Inicio     string    `json:"inicio"`           // HH:MM:SS (opcional)
	Fim        string    `json:"fim"`              // HH:MM:SS (opcional)
	Status     Estado    `json:"status"`
	CriadoEm   time.Time `json:"criado_em"`
	Erro       string    `json:"erro,omitempty"`

	// OrigemMs é o instante ABSOLUTO do vídeo do YouTube que corresponde ao t=0 do
	// arquivo video.mp4 deste pedido. Quem ESCREVE o vídeo declara — ver DeclararOrigem.
	//
	// Ponteiro, não int: zero é um valor LEGÍTIMO (vídeo inteiro) e precisa ser
	// distinguível de "ninguém declarou". Foi a confusão entre os dois que produziu o bug
	// de origem trocada — ver Origem().
	//
	// ATENÇÃO: descreve APENAS o arquivo em trabalho/<id>/video.mp4 (fluxo do cmd/baixar por
	// janela). O vídeo do CACHE (videos/<video_id>/video.mp4) tem a própria declaração, ao
	// lado dele, em video.json. Quem resolve qual arquivo e qual origem valem é o
	// videocache.Localizar — nunca leia este campo direto para renderizar (spec-09).
	OrigemMs *int `json:"origem_ms,omitempty"`

	// VideoID é o id do vídeo no YouTube (download.VideoID da URL). É a chave do cache:
	// videos/<VideoID>/ guarda o vídeo, a legenda e a transcrição ÍNTEGRA do culto, que
	// servem a qualquer janela e a qualquer pedido do mesmo vídeo.
	VideoID string `json:"video_id,omitempty"`

	// Recorte é a PROVENIÊNCIA do artefato derivado deste pedido: de qual vídeo e de qual
	// janela saiu a transcrição recortada em trabalho/<id>/transcricao.txt.
	//
	// Existe porque a transcrição fica em dois lugares (íntegra no cache, recortada no
	// pedido) e duas cópias do mesmo dado é como nasceram os dois bugs mais caros deste
	// projeto. A diferença aqui é que uma é DERIVÁVEL da outra — não são duas verdades
	// concorrentes. O que fecha o risco é declarar de onde a derivada veio: com isto, um
	// teste regenera o recorte a partir do cache e compara byte a byte (ver
	// internal/videocache). Se o vídeo for rebaixado e a íntegra mudar, é esse teste que
	// acusa, em vez de o operador descobrir por um Short estranho.
	//
	// O arquivo derivado NUNCA é editado: ou é regenerado, ou está errado.
	Recorte *Recorte `json:"recorte,omitempty"`
}

// Recorte registra de onde veio um artefato derivado da janela da pregação.
type Recorte struct {
	VideoID string `json:"video_id"` // qual vídeo do cache
	Inicio  string `json:"inicio"`   // HH:MM:SS — início da janela usada
	Fim     string `json:"fim"`      // HH:MM:SS — fim da janela usada
}

// DeclararRecorte registra a proveniência do recorte da transcrição.
func (p *Pedido) DeclararRecorte(videoID, inicio, fim string) {
	p.Recorte = &Recorte{VideoID: videoID, Inicio: inicio, Fim: fim}
}

// DeclararOrigem registra em que instante do vídeo original o video.mp4 deste pedido
// começa. Só quem escreve o arquivo sabe disso, então só quem escreve declara:
//
//	cmd/baixar (janela [inicio, fim])          -> origem = inicio
//	servidor / BaixarVideoCompleto (inteiro)   -> origem = 0
//
// O render LÊ este fato em vez de deduzi-lo. Ver Origem() para o porquê de não haver
// padrão.
func (p *Pedido) DeclararOrigem(origemMs int) {
	p.OrigemMs = &origemMs
}

// Origem devolve a origem de tempo declarada do video.mp4. Erro se ninguém declarou.
//
// NÃO existe padrão aqui, de propósito. O render antes assumia ped.Inicio, o que está
// certo para o vídeo baixado por janela (cmd/baixar) e ERRADO para o vídeo inteiro que o
// servidor baixa — e o pedido.json do servidor grava um Inicio real (o início da pregação),
// então a suposição não tinha como ser percebida. Resultado: `cmd/render -id <pedido do
// servidor>` gerava Shorts da cena errada, deslocados pelo Inicio, com a DURAÇÃO CORRETA —
// silencioso.
//
// Deduzir pela duração do arquivo também não serve: uma janela de 35 min e um vídeo inteiro
// de 35 min são indistinguíveis. Quando não há fato, o certo é falhar dizendo o que falta.
func (p *Pedido) Origem() (int, error) {
	if p.OrigemMs == nil {
		return 0, fmt.Errorf("pedido %q não declara a origem de tempo do video.mp4 (campo origem_ms "+
			"ausente em pedido.json). Sem isso não há como saber a que instante do vídeo original o "+
			"arquivo corresponde, e um corte deslocado sai com a duração certa e a cena errada. "+
			"Pedidos baixados antes desta versão não têm o campo: acrescente à mão o valor que o "+
			"arquivo de fato tem — 0 se o video.mp4 é o vídeo inteiro (o que o servidor baixa), ou o "+
			"início da janela em milissegundos se foi baixado por --download-sections (cmd/baixar). "+
			"Ou rebaixe o pedido, que a declaração passa a ser automática", p.ID)
	}
	return *p.OrigemMs, nil
}

// NovoPedido cria um pedido no estado inicial. O horário é injetado para manter
// a função testável e determinística (não chama time.Now internamente).
func NovoPedido(id, youtubeURL, inicio, fim string, criadoEm time.Time) *Pedido {
	return &Pedido{
		ID:         id,
		YouTubeURL: youtubeURL,
		Inicio:     inicio,
		Fim:        fim,
		Status:     EstadoRecebido,
		CriadoEm:   criadoEm,
	}
}

// Dir devolve a pasta de trabalho do pedido dentro de baseDir (ex.: "trabalho").
func (p *Pedido) Dir(baseDir string) string {
	return filepath.Join(baseDir, p.ID)
}

// Salvar grava o pedido em baseDir/<id>/pedido.json, criando a pasta se preciso.
func (p *Pedido) Salvar(baseDir string) error {
	if p.ID == "" {
		return fmt.Errorf("pedido sem ID")
	}
	dir := p.Dir(baseDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, nomeArquivo), b, 0644)
}

// Carregar lê baseDir/<id>/pedido.json.
func Carregar(baseDir, id string) (*Pedido, error) {
	b, err := os.ReadFile(filepath.Join(baseDir, id, nomeArquivo))
	if err != nil {
		return nil, err
	}
	var p Pedido
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
