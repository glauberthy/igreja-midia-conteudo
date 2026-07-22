# Spec 04 — Corte, reenquadramento 9:16 e legenda (ffmpeg encapsulado)

## Objetivo

Para cada candidato aprovado pelo validador, produzir o Short final: trecho cortado,
formato vertical 9:16 e legenda queimada, gravado em `finalizados/<id>/`. O `ffmpeg`
fica encapsulado no Go.

## Contexto

Depois da seleção (spec-02) e do download (spec-03), temos o vídeo do trecho da
pregação e os candidatos corrigidos, cada um com `start`/`end` confiáveis (já
corrigidos pelo validador). Falta transformar cada candidato num arquivo de vídeo
pronto para publicar. Ver BRD (produção/formato) e DP-005 (perfil visual) e DP-009
(enquadramento; sair do quadro é aceitável).

## Escopo

Dentro:
- Pacote `internal/video` que chama o `ffmpeg` como subprocesso.
- Cortar cada candidato do vídeo do trecho pelo `start`/`end` corrigido.
- Reenquadrar para 9:16 (vertical) conforme o perfil visual definido.
- Queimar a legenda do trecho no vídeo (a partir da legenda já baixada).
- Gravar cada Short em `finalizados/<id>/short_NN.mp4`.

Fora (specs futuras):
- Servidor/página e disparo (spec-05); retenção/limpeza (spec-06).

## Decisões já tomadas (não reabrir)

- **Alinhamento de tempo (CRÍTICO).** A transcrição usa tempos ABSOLUTOS do vídeo
  original; o `video.mp4` da spec-03 foi recortado com `--download-sections` e
  provavelmente começa em zero (linha de tempo rebaseada). Portanto, ao cortar um
  candidato de tempo absoluto `T`, o corte dentro do `video.mp4` **não** é em `T` — é
  em `T - inicio`. Antes de confiar nisso, **verificar empiricamente** com um vídeo
  real como o `video.mp4` recortado carimba o tempo, e fazer o corte bater. Não tratar
  como suposição: um erro aqui produz um Short com o vídeo de um trecho e a mensagem de
  outro — falha silenciosa que o validador não pega e que pode expor justamente uma
  posição refutada (risco RN-027). A legenda do trecho também precisa ser rebaseada com
  o mesmo deslocamento.
- Renderizar **todos** os candidatos validados do pedido (são poucos, ~3–5, e curtos).
  Isto substitui a ideia de prévia-antes-de-render do BRD (DP-004): como não há
  integração de WhatsApp e os clipes são baratos, o sistema entrega todos prontos em
  `finalizados/` e o operador escolhe quais enviar. Registrar essa substituição.
- Formato vertical 9:16; enquadramento fechado no pregador, sair do quadro é aceitável
  (DP-009). Perfil visual conforme DP-005.
- `ffmpeg` é dependência externa de sistema; o Go só o invoca.
- Nunca alterar o áudio/fala; legenda vem da transcrição, sem reescrever palavras.

## Passos de implementação

1. `internal/video/ffmpeg.go`: funções `Cortar`, `Reenquadrar9x16`, `QueimarLegenda`
   (ou uma pipeline única do ffmpeg que faça as três numa passada, se mais simples).
2. Gerar, para cada candidato, o arquivo de legenda do trecho (recorte da legenda pelo
   intervalo do candidato, com tempos rebaseados a zero).
3. Produzir `finalizados/<id>/short_NN.mp4` para cada candidato, em ordem de score.
4. Erros do ffmpeg → mensagem clara e `Status=erro`.
5. Comando `cmd/render` que, dado um `<id>` já baixado e com candidatos, gera os Shorts —
   para testar isolado.
6. Testes: execução do ffmpeg atrás de interface mockável; validar que o comando é
   montado com os parâmetros certos (intervalo, escala/crop 9:16, subtitle) e que a
   legenda do trecho é recortada e rebaseada corretamente (essa parte é lógica pura,
   testável sem ffmpeg).

## Contratos e interfaces

`Renderizar(ctx, pedido) ([]string, error)` — devolve os caminhos dos Shorts gerados
em `finalizados/<id>/`. Execução do ffmpeg atrás de interface `Executor` (mock nos
testes). A função de recorte/rebaseamento da legenda é pura e testada isoladamente.

Nome dos arquivos: `short_01.mp4`, `short_02.mp4`, ... na ordem de `score`.

## Critérios de aceite

- [ ] `internal/video` invoca o `ffmpeg` atrás de interface mockável.
- [ ] Para cada candidato validado, gera um `finalizados/<id>/short_NN.mp4`.
- [ ] Saída em 9:16; legenda do trecho queimada; intervalo = start/end corrigido.
- [ ] **Alinhamento verificado de verdade:** um teste manual ponta a ponta com um vídeo
      real curto confirma que o conteúdo do Short bate com o trecho da transcrição
      selecionado (conferir olhando: a fala no Short é a do `hook`). Documentar o
      deslocamento usado (rebase por `inicio`) e por quê.
- [ ] Recorte e rebaseamento da legenda do trecho testados como lógica pura.
- [ ] Erro de ffmpeg vira mensagem clara e `Status=erro`.
- [ ] README documenta a instalação do `ffmpeg`.
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
# manual, com ffmpeg instalado e um pedido já baixado (spec-03):
go run ./cmd/render -id teste
ls finalizados/teste/   # short_01.mp4, short_02.mp4, ...
```

## Fora de escopo / próximos passos

spec-05 — servidor HTTP e página do operador (entrada e acompanhamento).