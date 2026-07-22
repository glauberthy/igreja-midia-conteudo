# Metodologia de specs

Como escrevemos specs para o Claude Code executar neste projeto. A ideia é
**spec-driven development**: cada incremento é uma spec pequena, com escopo fechado
e critérios de aceite verificáveis. O agente executa a spec ativa, nada além dela.

## Princípios

1. **Uma spec por incremento.** Fatias pequenas e verificáveis. Nunca "faça o
   pipeline inteiro". Prefira "implemente a etapa X, com este contrato, validada por
   estes critérios".
2. **Escopo fechado.** A spec diz explicitamente o que está dentro e o que está fora.
   O que está fora, o agente não faz — vira spec futura.
3. **Decisões já tomadas não se reabrem.** A spec lista o que já foi decidido (no
   spike, no BRD) para o agente não redescobrir nem contradizer.
4. **Critérios de aceite testáveis.** Toda spec termina com uma checklist objetiva e
   os comandos exatos para verificar (build, teste, execução).
5. **Sem segredo, sem palavra do pregador alterada, validador obrigatório.** As
   regras do `CLAUDE.md` valem em toda spec.

## Template

Copie a estrutura abaixo para cada nova spec (`docs/specs/spec-NN-nome.md`):

```
# Spec NN — <título curto>

## Objetivo
Uma frase: o que esta spec entrega.

## Contexto
Por que agora, e de onde vem (BRD RN-xxx, aprendizados do spike). Links.

## Escopo
Dentro: <lista do que será feito>
Fora:   <lista do que NÃO será feito nesta spec; vira spec futura>

## Decisões já tomadas (não reabrir)
<lista das decisões fechadas que restringem a implementação>

## Passos de implementação
Ordenados, pequenos. Cada passo deve ser verificável isoladamente.
1. ...
2. ...

## Contratos e interfaces
Formatos de dados, assinaturas de função, formato de arquivos de entrada/saída.

## Critérios de aceite
Checklist objetiva. Cada item é verdadeiro ou falso, sem ambiguidade.
- [ ] ...
- [ ] `go build ./...` passa
- [ ] `go test ./...` passa

## Como validar
Comandos exatos a rodar e o resultado esperado.

## Fora de escopo / próximos passos
O que fica para a próxima spec.
```

## Dependências de sistema

Além do Go, a fase de produção usa ferramentas externas de linha de comando. Elas
**não** são módulos Go — o projeto apenas as invoca como subprocesso.

- **yt-dlp** (spec-03): baixa a legenda automática e o vídeo do YouTube.
  - Recomendado: `python3 -m pip install -U yt-dlp` (ou `pipx install yt-dlp`).
  - Alternativas: `sudo apt install yt-dlp` (Debian/Ubuntu recentes), `brew install yt-dlp` (macOS),
    ou o binário estático em https://github.com/yt-dlp/yt-dlp/releases.
  - Verifique com `yt-dlp --version`. O caminho do binário é configurável (`-bin`).
- **ffmpeg** (spec-03 e spec-04): o yt-dlp usa o ffmpeg para converter a legenda em
  `.srt` (`--convert-subs`) e para recortar/mesclar o vídeo; a spec-04 o usará direto.
  - `sudo apt install ffmpeg` / `brew install ffmpeg`. Verifique com `ffmpeg -version`.

Sem legenda automática em português, o download **para** com mensagem clara: não há
transcrição local (Whisper etc.). É uma decisão de projeto (BRD DP-001).

## Ordem prevista das specs (roadmap)

Cada spec é executada pelo Code na ordem, uma de cada vez, e aceita pelos seus
critérios antes da seguinte.

- **spec-01 — Fundação** (aceita): projeto Go limpo, testes, `.gitignore`.
- **spec-02 — Orquestração da seleção**: modelo de `Pedido` e fluxo
  srtclean → modelo → validar num pacote reutilizável. Sem vídeo.
- **spec-03 — Download e legenda**: `yt-dlp` encapsulado; baixa o trecho da pregação
  e gera a transcrição. Sem legenda pt → para (DP-001).
- **spec-04 — Vídeo 9:16 e legenda**: `ffmpeg` encapsulado; corta cada candidato,
  reenquadra vertical, queima legenda, grava em `finalizados/`.
- **spec-05 — Servidor e página**: HTTP em porta dedicada, três campos, processamento
  assíncrono com status; lista os Shorts prontos. Sem auth, sem integração de WhatsApp.
- **spec-06 — Retenção e limpeza**: descarta o vídeo bruto, preserva texto/logs.

Fluxo do operador (não é código): assiste ao culto e identifica início/fim da
pregação → abre a página, informa {link, início, fim} → aguarda → pega os Shorts de
`finalizados/` → envia ao pastor pelo WhatsApp Web, manualmente.
