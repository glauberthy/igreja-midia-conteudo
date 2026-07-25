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

## Confronto doutrinário dos trechos marcados (candidata a spec-14)

Ideia amadurecida numa conversa de 2026-07-24 sobre o indicador de fidelidade. Registrada
para não se perder antes do realinhamento das specs.

**Ponto de partida (esclarecimento importante):** julgar teologia JÁ é trabalho do modelo
(regra nº 5: "LLM para julgamento, código para o determinístico"). Hoje, na Fase 4, o
modelo lê o trecho COM a Declaração Doutrinária no prompt e dá a nota `context_fidelity`
(0–30). O que é determinístico é só a régua: nota `< 18` ou avaliações em duplicata
divergentes → marca `requer_revisao_reforcada` (⚠️), que já vai para a interface web. O
que falta é uma etapa mais rigorosa e específica sobre o que foi marcado.

**Proposta — uma "Fase 4.5: Confronto doutrinário"**, rodando SÓ nos trechos marcados:
- Entra: os trechos com `requer_revisao_reforcada` + a Declaração Doutrinária.
- O modelo faz um julgamento FOCADO e adversarial (ângulo diferente do score geral da
  Fase 4): "este trecho contradiz algum ponto da Declaração? Se sim, cite qual." Devolve
  algo como `{classe: fiel | desalinhamento[cita o ponto] | provável_erro_transcricao,
  motivo}`.
- Enriquece o `motivo_revisao` (vira específico e citável, não só um número) e PERSISTE os
  sinalizados num arquivo (ex.: `resultados/revisao-teologica.md` ou na pasta do pedido) —
  histórico auditável.
- A interface passa a mostrar o motivo citável junto do ⚠️.
- NUNCA descarta — só enriquece a marcação (mantém o espírito da spec-11).

**Por que agrega valor (não é repetir a Fase 4):**
1. Passo dedicado só à teologia, com prompt específico, é mais rigoroso que a nota geral
   (que mistura 5 critérios) e devolve motivo citável — muito mais útil ao pastor.
2. Ângulo adversarial ("ache o erro") pega coisa sutil que a nota geral deixa passar.
3. Bônus: pode SEPARAR "erro de doutrina" de "erro de transcrição". Perguntando "isto é
   problema doutrinário ou o texto está garbled?", tende a classificar casos como o
   "chiva ou chuva" (termo hebraico garbled pelo ASR) como provável erro de ASR, não
   heresia — reduzindo o falso positivo. (Preferimos falso positivo a passar algo ruim,
   mas distinguir as duas classes ajuda o operador.)

**Custo (viável na RTX 4000 Ada):** roda só nos trechos marcados (tipicamente 0–2 por
sermão); a Declaração já está no cache de prompt da Fase 4. ~1–2 chamadas curtas/sermão.

**Ressalva honesta:** continua sendo modelo confrontando modelo — reduz erro com uma lente
nova, não adiciona fonte de verdade nova. O pastor segue como certificador final (regra
nº 6). Por isso o passo marca/enriquece, nunca certifica nem descarta.

**Status:** registrado como candidata a spec-14. Vira spec quando o dono realinhar as
specs (há um prompt grande de realinhamento a caminho). Não implementar antes disso.

## Migrar o polling do servidor web para SSE (spec-05)

Hoje a página do operador acompanha o progresso das duas fases (leve e pesada) por
**polling**: `hx-trigger="every 2s"` refaz `GET /pedidos/{id}` de 2 em 2 segundos. Funciona
e é simples (HTMX sem build), mas a página pergunta o tempo todo mesmo quando nada mudou.

**Ideia:** um endpoint **SSE** (Server-Sent Events) no servidor Go que EMPURRA o estado
quando ele muda, com o HTMX como cliente do SSE (extensão `sse` do HTMX). Cobre as duas
fases (baixando-legenda → … → aguardando-aprovacao; baixando-video → renderizando →
concluido). Menos requisições, atualização instantânea, e o servidor deixa de responder a
N polls por pedido.

**Estado:** registrado. O código atual tem um comentário em `internal/servidor/templates.html`
apontando para cá. Fazer quando o polling incomodar (vários operadores/pedidos simultâneos)
ou junto de outra mexida na tela. Não urgente — o polling atende o uso esporádico de hoje.
