# Spec 01 — Fundação

## Objetivo

Organizar o que já foi construído e validado no spike (srtclean, validar, script de
lote, prompt) num projeto Go limpo, com estrutura padronizada, testes automatizados
e `.gitignore`. Nenhuma funcionalidade nova — só consolidar o que funciona.

## Contexto

O núcleo de seleção e validação já existe e foi validado com 5 sermões
(`docs/aprendizados-do-spike.md`). Hoje os arquivos estão soltos, sem testes e sem
proteção contra commit de segredos. Antes de construir a fase de produção, essa base
precisa estar organizada e confiável. Nada aqui reabre decisão do spike.

## Escopo

Dentro:
- Padronizar a estrutura de pastas conforme o `CLAUDE.md`.
- Renomear os arquivos de comando para `main.go` dentro de suas pastas.
- Adicionar testes de unidade para o srtclean e o validar.
- Criar `.gitignore` protegendo segredos e artefatos de teste.
- Garantir `go build ./...` e `go test ./...` verdes.

Fora (vira spec futura):
- Qualquer funcionalidade nova (orquestração, vídeo, entrega).
- Mudar o comportamento do srtclean ou do validar — só organizar e testar o que já há.

## Decisões já tomadas (não reabrir)

- srtclean: entrada `.srt`, saída `[HH:MM:SS] texto`, só tempo de início, remove
  marcação (tags e anotações), nunca altera palavras. Flag `-until` opcional.
- validar: dois modos (detectar e `-corrigir`). Correções: start deslizado → horário
  real do hook; hook inventado → descarta; score → soma dos critérios; duration →
  recalcula por `end - start`. Grava `.corrigido.json` sem tocar no original.
- Sem dependências externas nos comandos (só biblioteca padrão do Go).
- Parâmetros do modelo e formato do JSON: ver `docs/aprendizados-do-spike.md`.

## Passos de implementação

1. Criar a estrutura de pastas: `cmd/srtclean/`, `cmd/validar/`, `scripts/`,
   `prompts/`, `docs/`, `docs/specs/`, `testdata/`.
2. Mover `cmd/srtclean/srtclean_main.go` → `cmd/srtclean/main.go` e
   `cmd/validar/validar_main.go` → `cmd/validar/main.go`.
3. Mover `avaliar_sermoes.sh` → `scripts/`; ajustar caminhos internos se preciso
   (o prompt agora está em `prompts/`).
4. Mover o prompt de seleção → `prompts/selecao_shorts_v0_1.md`.
5. Mover 1–2 transcrições e SRTs de exemplo para `testdata/` (dados pequenos, sem
   informação sensível), para servirem aos testes.
6. Escrever testes de unidade (ver "Contratos"):
   - `cmd/srtclean/main_test.go`
   - `cmd/validar/main_test.go`
7. Criar `.gitignore`.
8. Rodar `go build ./...` e `go test ./...` e corrigir o que falhar.

## Contratos e interfaces

Testes do **srtclean** devem cobrir, no mínimo:
- Descarta numeração de sequência e setas `-->`.
- Remove tags `<i>...</i>`, `{\an8}` e anotações `[Música]`, `[Aplausos]`.
- Mantém as palavras faladas intactas (inclusive repetições legítimas).
- Usa o tempo de início; ignora blocos vazios.
- `-until` corta blocos a partir do tempo dado.
- Sobreposição de tempo do SRT não quebra a saída (inícios crescentes).

Testes do **validar** devem cobrir, no mínimo:
- Detecta campo obrigatório ausente (ex.: `score`).
- Detecta e, em `-corrigir`, ajusta start deslizado para o horário real do hook.
- Detecta e descarta candidato com hook inexistente na transcrição.
- Recalcula `score` como soma dos 5 critérios.
- Recalcula `duration_seconds` a partir de `end - start`.
- Preserva `mapa_sermao` e os candidatos válidos no `.corrigido.json`.

## Critérios de aceite

- [ ] Estrutura de pastas conforme o `CLAUDE.md`.
- [ ] Comandos em `cmd/srtclean/main.go` e `cmd/validar/main.go`.
- [ ] `scripts/avaliar_sermoes.sh` roda apontando para `prompts/` e `resultados/`.
- [ ] `.gitignore` cobre `resultados/`, `*.corrigido.json`, chaves/segredos
      (`*.key`, `openrouter.txt`, `.env`), e artefatos soltos (`payload.json`,
      `resp.json`).
- [ ] Testes de unidade para srtclean e validar, cobrindo os itens dos contratos.
- [ ] `go build ./...` passa sem erro.
- [ ] `go test ./...` passa, todos verdes.
- [ ] Nenhum comportamento existente foi alterado (mesma saída de antes nos exemplos).

## Como validar

```bash
go build ./...
go test ./...
go run ./cmd/srtclean -in testdata/exemplo.srt -out /tmp/exemplo.txt
go run ./cmd/validar -json testdata/candidatos_exemplo.json -transc /tmp/exemplo.txt
```

Esperado: build e testes verdes; srtclean gera o `.txt` no formato `[HH:MM:SS] texto`;
validar reporta (ou corrige) os problemas conhecidos sem quebrar.

## Fora de escopo / próximos passos

spec-02 — transformar srtclean → modelo → validar num comando único e reproduzível.
