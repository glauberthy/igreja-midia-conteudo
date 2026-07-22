# CLAUDE.md — Pipeline de Shorts de Sermões

Este arquivo é lido automaticamente pelo Claude Code. Ele descreve o projeto, as
regras invioláveis e como trabalhar aqui. Leia por completo antes de agir.

## O que é este projeto

Pipeline que transforma a gravação de um culto (já publicado no YouTube) em vídeos
curtos verticais (Shorts) para o canal da igreja. Um modelo de linguagem seleciona
os melhores trechos da pregação; um humano (pastor) aprova; o sistema renderiza.

O objetivo é **edificação e ensino**, não engajamento. Ver o BRD completo em
`docs/BRD_Pipeline_Shorts_Sermoes_v0_2.md`.

## Estado atual

O núcleo de **seleção e validação** já foi construído e validado num spike com 5
sermões de 5 pregadores diferentes. As decisões difíceis já estão tomadas — não as
reabra sem motivo. Ver `docs/aprendizados-do-spike.md`.

O que ainda **não** existe: download do vídeo, corte, reenquadramento 9:16, queima
de legenda, entrega ao pastor. Essa é a fase de produção, tratada em specs futuras.

## Stack

- **Go** (biblioteca padrão; sem dependências externas nos utilitários de linha de comando)
- **Bash + jq + curl** para orquestração e chamadas ao modelo
- **llama.cpp** (`llama-server`) servindo um modelo Gemma localmente
- **ffmpeg** será usado na fase de produção de vídeo (ainda não)

## Estrutura do projeto

```
cmd/srtclean/      # limpa legenda .srt -> transcrição [HH:MM:SS] texto
cmd/validar/       # valida e corrige o JSON de candidatos do modelo
scripts/           # avaliar_sermoes.sh (roda os sermões em lote)
prompts/           # prompt de sistema para a seleção
docs/              # BRD, aprendizados do spike, specs
docs/specs/        # specs de implementação (uma por incremento)
testdata/          # transcrições e SRTs de exemplo
resultados/        # saída das rodadas (NÃO versionar)
```

## Comandos

```bash
# Limpar uma legenda do YouTube
go run ./cmd/srtclean -in sermao.srt -out sermao.txt -until 00:33:10

# Rodar todos os sermões pelo modelo
./scripts/avaliar_sermoes.sh

# Validar (detectar problemas)
go run ./cmd/validar -de 1 -ate 5

# Validar e corrigir (gera .corrigido.json)
go run ./cmd/validar -de 1 -ate 5 -corrigir

# Testes
go test ./...
```

## Regras invioláveis

Estas regras não se negociam. Se uma tarefa parecer exigir quebrá-las, pare e pergunte.

1. **Teologia acima de engajamento.** A seleção prioriza fidelidade e ensino. Um
   trecho que engaja mas distorce a mensagem é um trecho ruim.

2. **Nunca alterar as palavras do pregador.** Limpeza de transcrição remove apenas
   marcação (tags, anotações), nunca fala. (BRD RN-013)

3. **O validador é obrigatório, não opcional.** Nenhum candidato do modelo chega a
   um humano sem passar pelo `validar`. O modelo erra timestamp, score e às vezes
   inventa o hook — o validador corrige o corrigível e descarta o resto.

4. **Segredos nunca no código nem na saída.** Chaves de API vão em variável de
   ambiente. Nada de chave em arquivo versionado, log ou JSON. (BRD RN-038)

5. **LLM só para julgamento; código para o determinístico.** Escolher trecho e
   avaliar fidelidade = modelo. Timestamp, soma de score, duração, parsing = código.
   Nunca peça ao modelo para fazer conta ou copiar número com precisão.

6. **Conteúdo sensível vai para revisão humana.** Trechos com afirmação doutrinária
   forte são marcados (`requer_revisao_reforcada`), nunca publicados automaticamente.

## Como trabalhar aqui

- Siga a spec ativa em `docs/specs/`. Uma spec por vez.
- Trabalho incremental: entregue fatias pequenas e verificáveis, não tudo de uma vez.
- Toda mudança de código precisa passar em `go build ./...` e `go test ./...`.
- Não reabra decisões já registradas em `docs/aprendizados-do-spike.md`.
- Prefira a biblioteca padrão do Go; só adicione dependência com justificativa.
