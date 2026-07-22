# Spec 05 — Servidor HTTP e página do operador

## Objetivo

Subir um servidor HTTP em Go, numa porta dedicada, que serve uma página simples com
três campos ({link, início, fim}). O operador preenche e clica; o servidor processa o
pedido inteiro em segundo plano (spec-02 a spec-04) e a página mostra o progresso e,
ao final, a lista dos Shorts prontos em `finalizados/`. Sem autenticação.

## Contexto

O operador não é técnico e não usa terminal. Ele já sabe navegar: abre o navegador na
porta X, cai direto na página, preenche o link e os tempos da pregação, e acompanha.
Ao terminar, ele mesmo pega os arquivos de `finalizados/` e envia pelo WhatsApp Web
(o sistema NÃO integra WhatsApp). Ver as decisões de entrada/entrega desta conversa.

## Escopo

Dentro:
- Servidor HTTP (biblioteca padrão) numa porta dedicada configurável (padrão `:7799`,
  não usar 80/8080/8000).
- Página única servida pelo Go: três campos + botão; sem framework, HTML/CSS/JS mínimos.
- Processamento assíncrono: aceitar o pedido, responder na hora com um `id`, e rodar
  o pipeline (download → seleção → validação → render) numa goroutine.
- Endpoint de status para a página consultar o progresso do pedido.
- Ao concluir, a página lista os Shorts de `finalizados/<id>/` com link para baixar.
- Sem autenticação (uso local, rede confiável).

Fora (spec futura):
- Retenção/limpeza de disco (spec-06).

## Decisões já tomadas (não reabrir)

- Porta dedicada, nunca 80/8080/8000. Configurável por flag/env, padrão `:7799`.
- Sem autenticação.
- Processamento é longo (minutos): a requisição de criação NÃO espera terminar; ela
  retorna um `id` e o trabalho segue em background. A página faz polling do status.
- O sistema termina no arquivo em `finalizados/`; a entrega ao pastor é manual, fora
  do sistema (WhatsApp Web pelo operador). Nada de integração de mensageria.
- Um pedido por vez é aceitável (2 operadores, uso esporádico); fila simples serve.

## Passos de implementação

1. `cmd/servidor/main.go`: sobe o HTTP na porta configurável; registra as rotas.
2. Rotas: `GET /` (página), `POST /pedidos` (cria, valida entrada, retorna `id`),
   `GET /pedidos/{id}` (status em JSON), `GET /finalizados/{id}/{arquivo}` (baixar).
3. Executor de pedidos em background (goroutine) que chama, em ordem, `Baixar`
   (spec-03), `Selecionar` (spec-02) e `Renderizar` (spec-04), atualizando `Status`.
4. Estados visíveis ao operador: `baixando`, `transcrevendo`, `selecionando`,
   `validando`, `renderizando`, `concluido`, `erro` (com mensagem clara em `erro`).
5. Página: formulário simples, validação básica no cliente (formato dos tempos), e
   uma área que faz polling em `GET /pedidos/{id}` e mostra progresso + lista final.
6. Testes: as rotas com `httptest`; validação de entrada; a máquina de estados do
   pedido (com download/seleção/render mockados). Sem subir o pipeline real nos testes.

## Contratos e interfaces

`POST /pedidos` recebe `{youtube_url, inicio, fim}`; valida; cria `Pedido`; enfileira;
responde `{id}`. `GET /pedidos/{id}` responde `{id, status, erro, shorts: [nomes]}`.
Página em `internal/web/` (HTML/CSS/JS embutidos via `embed`).

## Critérios de aceite

- [ ] Servidor sobe na porta configurável (padrão `:7799`), sem auth.
- [ ] `GET /` serve a página com três campos e botão.
- [ ] `POST /pedidos` valida entrada, cria o pedido e retorna `id` imediatamente
      (não bloqueia até o fim do processamento).
- [ ] `GET /pedidos/{id}` reflete o estado corrente do processamento.
- [ ] Ao concluir, a página lista os Shorts de `finalizados/<id>/` para baixar.
- [ ] Erro no pipeline aparece na página com mensagem clara.
- [ ] Testes de rotas e da máquina de estados com dependências mockadas.
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
go run ./cmd/servidor -porta :7799
# abrir http://localhost:7799 no navegador, preencher e acompanhar
```

## Refinamento futuro — ajuste editorial do corte pelo operador

Registrado a partir de teste real (short_01 do sermão `mg83gcM4ctw`): a Fase 3 produz um
`end` VÁLIDO (fim de frase, dentro de 30–58 s), mas o ponto de corte IDEAL é uma decisão
editorial que depende de ouvir. Ex.: a Fase 3 terminou o trecho em "...Isto é a graça que
levanta o incapaz" (36 s, válido e temático), mas o operador, ouvindo, preferiu terminar
uma frase antes, em "...para nos dar uma nova vida" (30 s, mais punchy). Nenhum é "errado"
— é gosto editorial. O código chega a um fim válido; o acabamento fino é humano.

Por isso, quando esta interface for além do básico, deve oferecer ao operador, para cada
Short, um **ajuste fino do início/fim**: ele ouve o trecho e apara o ponto de corte (ex.:
um controle de "encurtar fim / ajustar para [tempo]"). Ao confirmar, o servidor Go chama
o ffmpeg para re-cortar apenas aquele Short, sem refazer o pipeline. É rápido (recortar
~40 s leva segundos). Isto complementa a margem automática (spec-10): a margem resolve o
vazamento de frações de segundo por atraso de legenda; o ajuste manual resolve a escolha
editorial de qual frase é a última. Um é da máquina, o outro é do humano.

Não é escopo do MVP desta spec (que é: subir o servidor, aceitar o pedido, processar,
listar os Shorts). É um refinamento a implementar quando o fluxo básico estiver de pé.

## Fora de escopo / próximos passos

spec-06 — retenção do bruto e limpeza de disco.