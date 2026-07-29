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

## Como reportar

**Prova de entrega (obrigatória).** Ao final de cada tarefa, para **cada** item entregue,
forneça três coisas:

1. **Onde está** — arquivo e linha da mudança.
2. **Como eu confirmo sozinho** — um comando que o dono roda no terminal dele, cujo resultado
   seria **diferente** se a mudança não existisse. Não vale um comando que passa de qualquer
   jeito.
3. **O que esperar** — a saída esperada, para ele comparar.

Comece o relatório sempre pela saída de `git show --stat HEAD` (ou o intervalo de commits da
tarefa), para o dono ver de imediato quais arquivos foram tocados e conferir contra o que foi
afirmado.

**Não basta afirmar que fez, nem que os testes passaram.** Neste projeto, cinco testes passaram
com o bug presente, uma substituição de bloco falhou em silêncio e engoliu quatro funções, e um
`echo` declarou a suíte verde enquanto ela falhava. A verificação tem de ser observável pelo
dono, não uma afirmação sua.

**Se um item não tiver verificação observável** — comportamento que só aparece no navegador,
julgamento visual, dependência de rede — **diga isso explicitamente** e classifique como
"feito, não verificado", em vez de listá-lo junto com o que foi provado. Declarar honestamente
o que não foi verificado vale mais que uma lista uniforme de itens "prontos".

**Cuidado específico com valores de configuração:** verificar que uma constante existe não prova
que ela é usada. Neste projeto já aconteceu três vezes de um valor existir num lugar e o caminho
real usar outro (o render lendo o `pedido.json` antigo; a Fase 5 divergindo da Fase 3; o
`cmd/servidor` fixando `RodapeAlpha` e tornando a constante do pacote letra morta). Ao entregar
mudança de configuração, mostre o comando que prova o valor **no caminho que o operador usa**,
não só onde ele é declarado.
