# Aprendizados do spike

Registro do que foi descoberto testando a seleção com 5 sermões reais de 5
pregadores diferentes. **São decisões fechadas — não reabrir sem motivo novo.**
Serve para o Claude Code (e para nós) não repetir o caminho já andado.

## A pergunta que o spike respondeu

"Dá para confiar num modelo local para selecionar trechos de sermão para Shorts?"

Resposta, com evidência dos 5 sermões:
- **Para escolher trechos fiéis: sim.** Nenhum candidato foi infiel à doutrina em
  nenhum dos 5 sermões. O modelo mapeia o tema certo e respeita o contexto.
- **Para a precisão mecânica (timestamp, score, duração): não.** Erra com frequência.
- **Conclusão de arquitetura:** o modelo julga; o código valida e corrige. Por isso
  o validador é etapa obrigatória.

## O que funciona (manter)

- **Modelo:** Gemma servido localmente via `llama-server` (llama.cpp). Cabe bem uma
  transcrição de ~1h nos 64k de contexto configurados.
- **Parâmetros da chamada** (todos necessários — cada um resolveu um problema real):
  - `response_format: {type: "json_object"}` — força saída JSON.
  - `chat_template_kwargs: {enable_thinking: false}` — **crítico**. Sem isso, o
    Gemma gasta todo o `max_tokens` "pensando" em inglês e devolve `content` vazio.
  - `max_tokens: 3000` — suficiente para o JSON; sem teto, ele diverge.
  - `temperature: 0.2` — respostas estáveis entre rodadas.
  - `repeat_penalty: 1.1` — evita loop de repetição do modelo quantizado.
- **Prompt:** sistema no papel `system`, transcrição no papel `user` (separados).
  A declaração doutrinária da igreja vai embutida no prompt (parâmetro da seleção).
- **Transcrição:** limpar o SRT do YouTube antes (remove numeração, setas, tags e
  anotações como `[Música]`). Usar só o tempo de início de cada bloco.

## O que NÃO funciona (o validador cobre)

Frequência medida nos 5 sermões, antes dos ajustes de prompt: 16 problemas.
Depois dos ajustes de prompt: 9. Ou seja, o prompt reduz, mas não elimina.

- **Timestamp deslizado (persistente).** O modelo escolhe o trecho certo mas escreve
  um `start` 3–11s deslocado, pegando a linha vizinha. **Não é erro de truncamento
  do nosso srtclean** (verificado contra o SRT original: o srtclean está fiel). É
  limitação do modelo em copiar horários. → O validador reescreve o `start` com o
  horário real onde o `hook` aparece no texto.
- **Hook inventado.** Às vezes o modelo escreve no `hook` uma frase que não existe
  na transcrição a partir do `start`. Inaceitável (colocar palavra na boca do
  pregador). → O validador descarta o candidato.
- **Score errado.** O modelo escreve um `score` que não é a soma dos critérios. →
  O validador recalcula pela soma.
- **Duração acima de 60s / incoerente.** → O validador recalcula a partir de
  `end - start`. (Observação: se corrigir o start esticar o trecho além de 60s, o
  validador avisa mas não re-encurta — cortar é decisão de conteúdo.)

## Armadilhas já vividas (não repetir)

- Rodar sem `enable_thinking: false` → resposta vazia, 6 minutos de processamento.
- Rodar sem `max_tokens` → o modelo diverge e estoura o contexto.
- Colocar a persona no campo `role` em vez de `content` → JSON inválido.
- JSON com quebras de linha literais dentro de `-d '...'` no curl → inválido. Montar
  o payload com `jq` (escapa sozinho).
- Chave da OpenRouter colada em texto puro → comprometida. Sempre variável de ambiente.
- Contar com o timestamp do modelo → sempre desliza. Deixar o validador resolver.

## Contrato de saída do modelo (JSON de candidatos)

O modelo devolve um objeto com `mapa_sermao` e `candidatos[]`. Cada candidato:
`start`, `end`, `duration_seconds`, `score`, `hook`, `reason`, `complete_thought`,
`requer_revisao_reforcada`, e `criteria` com 5 subcampos (`context_fidelity`,
`pastoral_value`, `completeness`, `opening_strength`, `format_fit`).
`score` = soma dos 5 critérios. Pesos: fidelidade 30, valor pastoral 30,
completude 20, abertura 10, formato 10 (teologia > engajamento).
