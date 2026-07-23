# Spec 10 — Margem de recuo no corte final (evitar vazamento de áudio)

## Atualização (revisão do default para 0)

**O default da flag `-margem-fim` passou de 0,4 s para 0 s.** A investigação posterior
(ver spec do timestamp da legenda rolling) mostrou que a causa real de o Short terminar no
meio da frase NÃO é o vazamento que esta margem tratava, e sim o **timestamp adiantado da
legenda rolling**: o `srtclean` carimba cada linha com o *início* da cue, e nas legendas
de 2 linhas do YouTube a palavra final (linha de baixo) é falada só ao longo da cue —
então o `end` calculado fica 1–3 s ANTES do áudio real da última palavra. Nesse cenário, a
margem de 0,4 s **agrava** o corte (retira ainda mais do fim), em vez de ajudar.

Por isso o padrão passa a ser **não recuar** (corta no `end` cheio). A margem continua
existindo e configurável — é útil quando o `end` estiver preciso e realmente houver
vazamento da fala seguinte —, mas deixa de ser aplicada por omissão. A correção da causa
raiz (o timestamp adiantado) é tratada em spec própria; esta flag não é o lugar dela.

O restante do texto abaixo é o registro original da spec (mantido como histórico).

## Objetivo

Evitar que o Short capture o começo da fala seguinte no fim. Hoje, o corte termina no
`end` calculado (fim da frase, pelo timestamp da legenda), mas a legenda automática do
YouTube aparece com pequeno atraso em relação ao áudio — então, em vários trechos, o
áudio da próxima fala já começou quando o vídeo corta. Resultado: ~0,5 s da fala
seguinte vaza no fim.

## Contexto (diagnóstico confirmado)

Teste real com 4 Shorts do sermão `mg83gcM4ctw`: as durações batem com o pedido (33/37/
34/44 s), então NÃO é imprecisão de corte do ffmpeg (keyframe). O `end` cai no timestamp
onde a próxima legenda aparece; como a legenda atrasa em relação ao áudio, o corte pega o
início da fala seguinte. Padrão observado: short_01 cortou limpo (o fim da frase calhou
antes da próxima começar); short_02/03/04 vazaram (a próxima fala vinha colada). É um
descompasso legenda↔áudio, típico de legenda automática.

## Decisão (não reabrir)

Aplicar a margem **no corte (render)**, mantendo o `end` calculado pela Fase 3 intacto.
A Fase 3 continua marcando a verdade (onde a frase termina); o render apara uma pequena
margem antes desse ponto, para não capturar a fala seguinte. Responsabilidades separadas:
seleção marca o fim real; render aplica o ajuste prático.

Isto é a camada AUTOMÁTICA (resolve a maioria dos casos, barato). O ajuste fino manual
(operador ouve e apara na interface web) será tratado na spec da interface web (spec-05)
— ver "Nota sobre o ajuste manual".

## Escopo

Dentro:
- `cmd/render` / `internal/video`: ao cortar cada Short, terminar em `end - margem`, com
  `margem` configurável (flag `-margem-fim`). **Default 0** (corta no `end` cheio) — ver a
  atualização no topo; historicamente foi 0,4 s.
- A margem nunca pode inverter o trecho (se `end - margem <= start`, não aplicar / erro
  claro — não deve acontecer com trechos de 30–58 s, mas guardar contra isso).
- Log deixando claro que a margem foi aplicada e qual valor.

Fora:
- Lógica de cálculo do `end` na Fase 3 (não muda — o `end` continua marcando fim de frase).
- Ajuste manual por Short (é da interface web / spec-05).

## Trade-off registrado (aceito)

Recuar o fim em `margem` para TODOS os Shorts significa que os que já cortavam limpo
perdem `margem` segundos do fim (ex.: 0,4 s de silêncio/respiração). É aceitável: some um
pequeno silêncio final, em troca de nunca vazar a fala seguinte. A margem é configurável
para calibrar esse equilíbrio.

## Critérios de aceite

- [ ] Corte termina em `end - margem`; `margem` configurável por flag, **default 0**
      (sem recuo por omissão — ver atualização no topo).
- [ ] Nos 4 Shorts do sermão de referência, o áudio da fala seguinte não é mais audível
      no fim (verificação do operador, ouvindo).
- [ ] A última frase do trecho continua completa (a margem não corta sílaba final —
      calibrar se necessário; se 0,4 s cortar, testar 0,3 s).
- [ ] Guarda contra margem que inverta o trecho.
- [ ] `go test ./...` verde, com teste do cálculo `end - margem` (inclusive o guard).

## Como validar

```bash
go test ./...
rm -f finalizados/sermao/short_*.mp4
go run ./cmd/render -id sermao               # default 0: corta no end cheio (durações inteiras)
# se, com o end preciso, ainda houver vazamento da fala seguinte, aplicar margem:
go run ./cmd/render -id sermao -margem-fim 0.3
```

## Nota sobre o ajuste manual (futuro — spec-05, interface web)

A ideia do operador ouvir e aparar o fim na mão (clicar um botão que chama o ffmpeg para
re-cortar aquele Short) é a solução PERFEITA para os casos que a margem automática não
resolver. Ela pertence à interface web do operador (spec-05): lá o operador já vai revisar
cada Short, então um controle de "aparar fim / ajustar para [tempo]" é um acréscimo
natural. Registrar como requisito da spec-05 quando ela for escrita. A margem automática
desta spec-10 é o "bom o suficiente" imediato; o ajuste manual é o refinamento fino.
