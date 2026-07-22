# Spec 06 — Retenção do bruto e limpeza de disco

## Objetivo

Evitar que o disco encha: descartar o vídeo bruto (o arquivo grande) quando ele não é
mais necessário, mantendo texto e logs por um prazo. Fecha o ciclo operacional.

## Contexto

O vídeo baixado do culto é o maior consumidor de disco e só serve para gerar os
Shorts. Depois que os finalizados existem, o bruto pode ir embora. Texto (transcrição,
candidatos) e logs são leves e úteis para auditoria. Ver BRD DP-007 (vídeo retido só
enquanto necessário; texto/logs retidos). O conceito de "sermão em aberto" do BRD
nunca foi formalizado; aqui adotamos uma regra concreta e simples no lugar.

## Escopo

Dentro:
- Descartar `trabalho/<id>/video.mp4` (e a legenda bruta) após os Shorts finalizados
  serem gerados com sucesso — ou após um prazo de retenção configurável, o que vier
  primeiro.
- Manter `transcricao.txt`, `candidatos*.json` e logs.
- Uma rotina de limpeza acionável (comando + opção de execução periódica simples).

Fora:
- Qualquer política jurídica de retenção de dados (não se aplica; culto é público —
  decisão já registrada no BRD). Aqui é só higiene de disco.

## Decisões já tomadas (não reabrir)

- O bruto é descartável assim que os finalizados existem. (BRD DP-007)
- Texto e logs são retidos.
- "Sermão em aberto" (conceito abstrato do BRD, nunca definido) é substituído por uma
  regra concreta: bruto vai embora quando `finalizados/<id>/` tem ao menos um Short,
  ou quando passa o prazo de retenção configurável (padrão: 7 dias). Registrar.

## Passos de implementação

1. `internal/retencao/limpeza.go`: função que, para cada `<id>`, remove o bruto quando
   a condição de descarte é satisfeita; preserva texto/logs.
2. Prazo de retenção configurável (flag/env), padrão 7 dias.
3. `cmd/limpar` que roda a limpeza uma vez (para cron/manual) e reporta o que removeu.
4. Opcional: acionar a limpeza também ao final de cada pedido concluído (no servidor).
5. Testes: com um diretório de trabalho simulado, verificar que o bruto é removido nas
   condições certas e que texto/logs sobrevivem; verificar o respeito ao prazo.

## Contratos e interfaces

`Limpar(ctx, raizTrabalho, prazo) (removidos []string, err error)` — remove brutos
elegíveis, devolve o que foi removido. Idempotente (rodar de novo não quebra).

## Critérios de aceite

- [ ] Bruto removido quando há Short em `finalizados/<id>/` ou após o prazo.
- [ ] `transcricao.txt`, `candidatos*.json` e logs preservados.
- [ ] Prazo de retenção configurável (padrão 7 dias).
- [ ] `cmd/limpar` roda, reporta o removido e é idempotente.
- [ ] Testes cobrem descarte, preservação e respeito ao prazo (sem tocar disco real
      além de `t.TempDir`).
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
go run ./cmd/limpar -prazo 168h   # 7 dias
```

## Fora de escopo / próximos passos

Pipeline completo. Melhorias futuras (legenda palavra-a-palavra, modelo externo
opcional, métricas) entram como specs novas quando/se necessárias.
