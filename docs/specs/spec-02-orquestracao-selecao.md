# Spec 02 — Modelo de pedido e orquestração da seleção

## Objetivo

Definir a estrutura de um "pedido" ({link, início, fim, status}) e juntar o núcleo já
validado (srtclean → modelo → validar) num único fluxo Go que, a partir de uma
transcrição limpa, produz os candidatos já corrigidos. Sem vídeo ainda.

## Contexto

O srtclean e o validar já existem, testados (spec-01). Hoje são chamados soltos, à
mão. Esta spec cria a "cola" que os encadeia num fluxo reproduzível e o modelo de
dados que todas as specs seguintes vão usar. Ver `docs/aprendizados-do-spike.md` para
os parâmetros do modelo e o contrato do JSON.

## Escopo

Dentro:
- Tipo `Pedido` em Go (pacote `internal/pipeline`), com estados e persistência simples.
- Layout de diretórios de trabalho por pedido (ver Contratos).
- Função de orquestração `Selecionar(transcricao) -> candidatos corrigidos` que chama,
  em Go, a mesma lógica do modelo (via `llama-server`) e do validar-corretor.
- Chamada ao `llama-server` a partir do Go (substitui o `avaliar_sermoes.sh` no fluxo
  automatizado; o script continua existindo para testes manuais).

Fora (specs futuras):
- Download de vídeo (spec-03), corte/render (spec-04), servidor web (spec-05).

## Decisões já tomadas (não reabrir)

- Parâmetros do modelo: `temperature 0.2`, `max_tokens 3000`, `repeat_penalty 1.1`,
  `response_format json_object`, `chat_template_kwargs.enable_thinking false`.
- Prompt de sistema em `prompts/selecao_shorts_v0_1.md`; transcrição vai no papel `user`.
- Endpoint do modelo configurável (padrão `http://localhost:8080/v1/chat/completions`).
- O validar corrige start/score/duração e descarta hook inventado — reusar a lógica
  existente, não reescrever.
- Chave de API (se o modo externo for usado) só via variável de ambiente.

## Passos de implementação

1. Criar `internal/pipeline/pedido.go` com o tipo `Pedido` e funções de salvar/carregar.
2. Criar `internal/pipeline/selecao.go` com `Selecionar`, que monta o payload, chama o
   `llama-server`, recebe o JSON de candidatos e aplica a validação-correção.
3. Refatorar a lógica de validação/correção do `cmd/validar` para um pacote reutilizável
   (`internal/validacao`) chamado tanto pelo comando quanto pela orquestração. O
   comando `cmd/validar` continua funcionando igual (fina camada sobre o pacote).
4. Idem para o srtclean, se fizer sentido reusar (`internal/transcricao`).
5. Um comando `cmd/selecionar` que recebe uma transcrição e imprime/salva os candidatos
   corrigidos — para testar o fluxo ponta a ponta da seleção sem vídeo.
6. Testes: orquestração com um `llama-server` "fake" (servidor HTTP de teste que
   devolve um JSON conhecido), verificando que a correção é aplicada.

## Contratos e interfaces

Tipo `Pedido` (mínimo): `ID` (string), `YouTubeURL` (string), `Inicio` (string HH:MM:SS),
`Fim` (string HH:MM:SS), `Status` (enum), `CriadoEm` (timestamp), `Erro` (string),
`Candidatos` (resultado corrigido). Estados: `recebido`, `selecionando`, `validando`,
`concluido`, `erro` (mais estados de vídeo entram na spec-03/04).

Layout de diretórios:
```
trabalho/<pedido_id>/
  legenda.srt          # da spec-03
  transcricao.txt      # da spec-03 (srtclean)
  candidatos.json      # saída crua do modelo
  candidatos.corrigido.json
finalizados/<pedido_id>/   # Shorts prontos (spec-04)
```

`Selecionar(ctx, transcricaoPath) ([]Candidato, error)` — devolve candidatos já
corrigidos; erro claro se o modelo devolver vazio/JSON inválido.

## Critérios de aceite

- [ ] Tipo `Pedido` com salvar/carregar em JSON, com teste.
- [ ] `internal/validacao` reusado pelo `cmd/validar` sem mudar seu comportamento
      (testes da spec-01 continuam verdes).
- [ ] `Selecionar` chama o modelo, aplica correção e devolve candidatos válidos.
- [ ] Teste da orquestração usando `httptest` como modelo fake (sem depender do
      `llama-server` real).
- [ ] `cmd/selecionar -transc testdata/exemplo.txt` roda o fluxo e salva o corrigido.
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
# com o llama-server rodando:
go run ./cmd/selecionar -transc transcricao_1.txt -out trabalho/teste/candidatos.json
```

## Fora de escopo / próximos passos

spec-03 — baixar o vídeo e extrair a legenda (yt-dlp encapsulado).
