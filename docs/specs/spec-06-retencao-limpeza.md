# Spec 06 — Retenção do bruto e limpeza de disco

## Objetivo

Evitar que o disco encha: descartar o vídeo bruto (o arquivo grande) dos pedidos
anteriores, mantendo o histórico auditável (texto, candidatos validados, Shorts) e o
bruto do pedido mais recente. Fecha o ciclo operacional.

## Contexto

O vídeo baixado do culto é o maior consumidor de disco e só serve para gerar os
Shorts. Depois que os finalizados existem, o bruto pode ir embora. Texto (transcrição,
candidatos) e logs são leves e úteis para auditoria. Ver BRD DP-007 (vídeo retido só
enquanto necessário; texto/logs retidos). O conceito de "sermão em aberto" do BRD
nunca foi formalizado; aqui adotamos uma regra concreta e simples no lugar.

## Escopo

Dentro:
- Descartar `trabalho/<id>/video.mp4` e demais brutos dos pedidos ANTERIORES, mantendo
  intactos os N mais recentes (padrão 1) — ver a política em "Decisões".
- Manter `transcricao.txt`, `candidatos.corrigido.json`, `revisao-teologica.json`,
  `pedido.json` e tudo em `finalizados/` e `resultados/`.
- Duas formas de acionar: automática (ao concluir um pedido) e manual (`cmd/limpar`,
  com `-dry-run`).

Fora:
- Qualquer política jurídica de retenção de dados (não se aplica; culto é público —
  decisão já registrada no BRD). Aqui é só higiene de disco.

## Decisões já tomadas (não reabrir)

- O bruto é descartável assim que os finalizados existem. (BRD DP-007)
- Texto e logs são retidos.
- "Sermão em aberto" (conceito abstrato do BRD, nunca definido) é substituído por uma
  regra concreta.
- **A política é por CONTAGEM DE PEDIDOS, não por prazo** (mudança em relação ao rascunho
  desta spec, que falava em 7 dias). Mantém-se o bruto dos N pedidos mais recentes
  (padrão N=1) e limpa-se o resto. Motivo: com ~571 MB por pedido (medido), um prazo de 7
  dias significa "quantos pedidos couberem em 7 dias" — imprevisível, porque depende da
  frequência de uso. Por contagem, o teto de disco é conhecido: N × ~571 MB. Manter o
  último permite regerar um Short sem baixar de novo; mais que isso volta a acumular.
- **A recência vem dos arquivos PRESERVADOS do pedido, não do mtime da pasta.** Apagar
  arquivos atualiza o mtime do diretório — se a ordem viesse dali, o pedido recém-limpo
  viraria "o mais recente" e a limpeza seguinte comeria justamente o que deve ser retido.
  (Bug real, pego por teste durante a implementação.)

## Passos de implementação

1. `internal/retencao/limpeza.go`: aplica a política e remove o bruto; preserva o
   histórico auditável.
2. Retenção configurável (`-reter`, padrão 1 pedido).
3. `cmd/limpar` (manual/cron) com `-dry-run`, que reporta o que removeu e quanto liberou.
4. Acionamento automático ao final de cada pedido concluído (no servidor), mantendo o
   pedido atual como intocável.
5. Testes: política, whitelist de preservação, guarda de caminho, dry-run, idempotência.

## Contratos e interfaces

`retencao.Limpar(Opcoes{RaizTrabalho, Reter, Intocaveis, DryRun}) (Resultado, error)` —
remove os brutos elegíveis e devolve o que foi removido, os bytes liberados e os retidos.
Idempotente (rodar de novo não quebra nem re-conta).

**Apagados** (bruto regenerável): `video.mp4`, `legenda.srt`, `legenda.info.json`,
`short_NN.subNNN.txt`, `mapa.json`, `candidatos_brutos.json`, `candidatos_delim.json`,
`*.part`/`*.ytdl`.

**Preservados SEMPRE** (histórico auditável): `candidatos.corrigido.json` (fonte de verdade
validada, spec-09), `transcricao.txt` (insumo do `cmd/auditar`), `revisao-teologica.json`
(spec-14), `pedido.json`. E, por construção, tudo em `finalizados/` e `resultados/` — que
estão FORA da raiz de trabalho, a única pasta que a limpeza enxerga.

**Segurança:** a checagem de preservados vence a de removíveis (se alguém puser um arquivo
protegido na lista de remoção, ele continua protegido — coberto por teste); `caminhoSeguro`
recusa travessia (`..`, separadores, caminho absoluto) e confere que o destino está sob a
raiz; o pedido em curso entra como intocável.

## Critérios de aceite

- [x] Bruto removido dos pedidos anteriores; os N mais recentes ficam intactos.
- [x] `candidatos.corrigido.json`, `transcricao.txt`, `revisao-teologica.json` e
      `pedido.json` preservados; `finalizados/` nunca tocado.
- [x] Retenção configurável (`-reter`, padrão 1).
- [x] `cmd/limpar` roda, tem `-dry-run`, reporta o liberado e é idempotente.
- [x] Limpeza automática ao concluir um pedido, com o atual intocável.
- [x] Testes cobrem política, whitelist, guarda de caminho, dry-run e idempotência
      (sem tocar disco real além de `t.TempDir`).
- [x] `go build ./...` e `go test ./...` verdes.

**Resultado da limpeza retroativa (2026-07-26):** `trabalho/` foi de **4,0 GB para 126 MB**
— **3,9 GB liberados** em 7 pedidos, retendo o mais recente. `finalizados/` (32 Shorts,
417 MB) e `resultados/tempos.csv` intactos.

## Como validar

```bash
go test ./...
go run ./cmd/limpar -dry-run      # mostra o que faria e quanto liberaria
go run ./cmd/limpar               # limpa, retendo o pedido mais recente
go run ./cmd/limpar -reter 3 -v   # retém 3 e lista os arquivos
```

## Fora de escopo / próximos passos

Pipeline completo. Melhorias futuras (legenda palavra-a-palavra, modelo externo
opcional, métricas) entram como specs novas quando/se necessárias.
