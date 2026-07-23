# Ideias futuras (registradas para não se perder)

## Inverter a ordem do pipeline: selecionar ANTES de baixar o vídeo

**Origem:** provocação do dono do projeto durante os testes de ponta a ponta.

**Observação que motiva:** a seleção por LLM é 100% texto (usa só a transcrição/legenda,
que é leve). Hoje o pipeline baixa o vídeo INTEIRO primeiro (caro: banda, tempo, disco) e
só depois seleciona. Ou seja, paga-se o custo caro antes de saber se vale a pena.

**Fluxo proposto (redesenho da spec-05):**
1. Baixar apenas a **legenda/transcrição** (texto, leve, rápido).
2. Rodar a seleção completa (harness 5 fases) → produz os trechos com `start`/`end`.
3. **Pré-visualização sem baixar vídeo:** a página do operador mostra cada trecho
   candidato embedado do **player do YouTube**, usando os parâmetros `start` e `end` da
   URL. O operador assiste os trechos direto do YouTube — o sistema ainda não baixou nada.
4. O operador revisa, **ajusta os tempos se quiser** (é o lugar natural do ajuste
   editorial do corte — ver refinamento da spec-05), e **aprova** os trechos que quer.
5. **Só após a aprovação**, o sistema baixa o vídeo (idealmente só os trechos aprovados,
   via `--download-sections` do yt-dlp) e faz o corte local preciso + legenda.

**Ganhos:** baixa só o aprovado (economia grande); revisão humana antes do processamento
pesado; a pré-visualização vira o lugar do ajuste editorial de corte.

**Pegadinhas registradas:**
- Depende de legenda automática existir (já tratado por DP-001: sem legenda → para).
- Preview via player do YouTube é aproximado: não corta de verdade, precisão de segundo
  (não frame), depende de conexão na hora da revisão. Suficiente para revisão humana, mas
  não é o corte final (que continua local e preciso).
- É um **redesenho da spec-05** (a interface) + muda a orquestração do pipeline, que passa
  a ter uma pausa para aprovação humana no meio: (fase 1) baixar-legenda → selecionar →
  apresentar; (aprovação); (fase 2) baixar-trechos → renderizar. Não é ajuste pequeno.

**Status:** registrado. A implementar quando for redesenhar a spec-05, após validar o
pipeline atual de ponta a ponta.
