# Otimização do modelo local (RTX 4000 Ada, 20 GB)

Objetivo: rodar a **seleção de Shorts** (harness fases 1→5) o mais rápido e estável
possível na placa do operador, sem penalizar a qualidade teológica. Não é fine-tuning:
é ajuste de **configuração do llama-server**, **sampling** e, se necessário, **prompt**.

## Ambiente

- **GPU:** NVIDIA RTX 4000 Ada, **20 GB** VRAM. 24 cores CPU, 62 GB RAM.
- **llama.cpp:** build 9569.
- **Modelos:**
  - Gemma 26B-A4B (MoE, ~4B ativos) Q4_K_XL — **14.25 GB** + draft Q4_0 **321 MB** (speculative decoding).
  - Qwen3.5 35B-A3B (MoE, ~3B ativos) Q3_K_XL — **16.6 GB**.
- **Scripts:** `~/start-gemma.sh`, `~/start-qwen.sh` (backup feito antes de qualquer ajuste).

## Diagnóstico de dimensionamento (medido)

- Prompts: fase1 ~632 tok, fase2 ~877 tok, fase4 ~682 tok.
- **Declaração Doutrinária: ~8.5k tokens**, anexada em toda chamada da Fase 4 (2×/candidato).
  Fica no *system prompt* (prefixo constante) → o cache de prompt do llama-server a
  reaproveita entre as chamadas do mesmo sermão.
- Maior transcrição **desduplicada**: ~9.3k tokens (era ~25k antes do dedup).
- **Pico de contexto (Fase 2, maior sermão): ~15.8k tokens** (input+saída).
- **Conclusão:** contexto de 64k (Gemma) / 40k (Qwen) é ~3-4× o necessário. Cortar para
  **~24k** libera VRAM (menos KV cache), acelera e evita OOM. → Hipótese H1.

## Hipóteses

- **H1 — Contexto sobredimensionado.** Reduzir `-c` de 64k/40k → 24k libera VRAM, acelera
  o prompt processing e elimina o risco de OOM (provável causa do crash do Qwen).
- **H2 — Gemma Q4 + draft > Qwen Q3** em estabilidade e velocidade nesta placa (quant
  menos agressivo; speculative decoding com draft de 321 MB).
- **H3 — Estabilidade da saída.** Sampling/penalidades adequados evitam loop/truncamento
  da Fase 2 (visto no Qwen Q3).

## Método

Dirijo o pipeline real via `cmd/harness -ate 5` contra o `llama-server` local (mede fase
a fase e loga retries/truncamentos). Para variância, repito o mesmo sermão N vezes e
comparo os candidatos finais (sobreposição do topo, dispersão de score). VRAM por
`nvidia-smi`. Sermões de teste: as 3 rodadas já coletadas (links no `resultados/`).

---

## Resultados (medidos)

### Config recomendada — Gemma 26B-A4B, 24k, 1 slot

`~/start-gemma-otim.sh` (backup do original em `~/start-gemma.sh.bak-*`):

| Métrica | Valor |
|---|---|
| **VRAM** | **16.2 GB / 20 GB** (3.8 GB livres) |
| Slots / contexto | 1 slot com **24576** tokens (contexto inteiro) |
| Prompt processing | **~2950 tokens/s** |
| Geração | **~120-125 tokens/s** (speculative decoding, draft acceptance **0.70-0.79**) |
| **Funil completo (Fase 1→5)** | **24.8 s** (menor sermão) a **35.8 s** (maior) |
| Estabilidade | **0 retries, 0 truncamentos, 0 crashes** em todas as execuções |

Detalhe do maior sermão (~9.3k tokens desduplicados):
- Fase 1 (mapa): prompt 13k tok → ~12.5 s.
- Fase 2 (candidatos): prompt 14k tok → ~8.2 s.
- Fase 4 (avaliação ×2/candidato): 1ª chamada processa a Declaração (13k tok, ~5.4 s); as
  demais reusam o **prompt cache** (prompt eval de 1-90 tok) → ~1 s cada.

### O que mudou e por quê

1. **Contexto 64k → 24k** (`-c 24576`). Pico real medido (Fase 2, maior sermão) = ~16k
   tokens; 64k era ~4× o necessário. Libera VRAM (menos KV cache) e remove risco de OOM
   (provável causa do crash do Qwen). O `n_ctx_train` do Gemma é 262k, então 24k é folgado
   para o nosso uso.
2. **`--parallel 1`**. Sem isso o llama.cpp abre **4 slots** e divide o contexto (24k/4 =
   6k por slot — pequeno demais; e no script antigo, 64k/4 = 16k, *no limite* do pico).
   Com 1 slot, o contexto inteiro fica disponível para a única requisição em curso.
3. **Mantido**: speculative decoding (draft de 321 MB, aceitação ~0.75 → ~2× na geração),
   flash-attn, KV cache q8_0. O **prompt cache** do llama-server amortiza a Declaração
   Doutrinária (8.5k tok) entre as chamadas da Fase 4.

### Variância (mesmo sermão, várias execuções)

| | temp 0.2 (padrão) | temp 0.0 (greedy) |
|---|---|---|
| Trechos idênticos entre runs | 1 de ~5 | **top-3 idênticos** |
| Nº de candidatos por run | 5 / 5 / 3 | 4 / 3 |

- As **regiões/temas** do sermão são estáveis mesmo em temp 0.2 (os mesmos blocos recorrem);
  o que varia é a **frase-âncora exata** e a **contagem** de candidatos.
- **temp 0.0 torna a seleção quase determinística**: os 3 melhores trechos (scores mais
  altos) saem idênticos entre execuções, **sem perder qualidade** (os melhores sempre
  emergem). A variância residual (4 vs 3) vem do **não-determinismo de hardware** (ponto
  flutuante na GPU + roteamento do MoE), não do sampling nem do speculative decoding —
  medido: temp 0 SEM o draft model deu 4/5/6 (não melhorou; o draft não é o culpado).
- A temperatura é **configurável por ambiente** (`HARNESS_TEMP`, `HARNESS_REPEAT_PENALTY`)
  — sem recompilar. **O default passou a ser 0** (era 0.2), por auditabilidade — ver a
  decisão na spec-05 ("Temperatura padrão = 0"). Medição posterior (3× no `IxmiQGL9CMQ`):
  **3/3 runs idênticos** a temp 0. Ressalva: não é garantia exata — o sermão grande
  `174206-3` deu 4 vs 3 a temp 0 (resíduo de hardware). Para explorar outros trechos,
  re-rodar com `HARNESS_TEMP=0.5` ("buscar outros trechos", spec-05).

### Recomendação

- **Modelo:** Gemma 26B-A4B Q4_K_XL nesta placa — rápido (25-36 s/sermão), estável e com
  boa qualidade teológica (scores altos, sem marcações de fidelidade nos testes). O Qwen
  Q3 é mais arriscado (quant agressivo; loop/crash antes do dedup + contexto reduzido).
- **Config:** `~/start-gemma-otim.sh` (24k, 1 slot).
- **Reprodutibilidade:** default `HARNESS_TEMP=0` (decisão na spec-05) — mesma entrada,
  mesma saída na prática, o que dá auditabilidade ("se mudou, alguém mexeu"). Para *mais
  cobertura* pontual, o operador re-roda com `HARNESS_TEMP=0.5`.
- **Ideia futura (não implementada):** rodar a seleção 2× e unir os candidatos (dedup por
  região) — combina cobertura alta E estabilidade, ao custo de ~2× o tempo.

### Pendente (opcional)

- Comparação cabeça-a-cabeça com **Qwen 24k** (exige reiniciar o servidor com o Qwen). O
  Gemma já atende bem; a comparação só vale se quiser confirmar o trade-off qualidade×velocidade.
