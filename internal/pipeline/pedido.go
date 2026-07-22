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

	"srtclean/internal/validacao"
)

// Estado é o ponto do ciclo de vida em que o pedido está. Os estados de vídeo
// (baixando, cortando, renderizando, entregue) entram nas specs 03/04/05.
type Estado string

const (
	EstadoRecebido     Estado = "recebido"
	EstadoSelecionando Estado = "selecionando"
	EstadoValidando    Estado = "validando"
	EstadoConcluido    Estado = "concluido"
	EstadoErro         Estado = "erro"
)

// nomeArquivo é o JSON de metadados do pedido dentro da pasta de trabalho.
const nomeArquivo = "pedido.json"

// Pedido é a unidade de trabalho do pipeline. É serializável em JSON e serve de
// contrato entre as etapas e as specs seguintes.
type Pedido struct {
	ID         string                `json:"id"`
	YouTubeURL string                `json:"youtube_url"`
	Inicio     string                `json:"inicio"` // HH:MM:SS (opcional)
	Fim        string                `json:"fim"`    // HH:MM:SS (opcional)
	Status     Estado                `json:"status"`
	CriadoEm   time.Time             `json:"criado_em"`
	Erro       string                `json:"erro,omitempty"`
	Candidatos []validacao.Candidato `json:"candidatos,omitempty"`
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
