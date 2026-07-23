# Pendências do projeto — lista priorizada

Documento vivo. Atualizar conforme itens forem concluídos. A ordem reflete prioridade
(qualidade e fidelidade teológica acima de tudo; tempo não é restrição).

Última atualização: sessão de 23/07/2026.

---

## EM ANDAMENTO / RECÉM-FEITO

- [x] **spec-11 — fidelidade marca em vez de vetar** (código). FEITO e validado: João 17
      volta marcado em vez de sumir. (Commit pendente de confirmação.)
- [x] **Ajuste do prompt da Fase 4 — reconhecer citação bíblica** (afinação). Prompt
      ajustado (arquivo `fase4_avaliacao.md` novo). FALTA: substituir na máquina e testar
      no culto-noite (esperado: João 17 recebe fidelidade alta, sobe na lista, deixa de
      ser marcado).

## PRIORIDADE ALTA (qualidade do produto — o Short em si)

1. **Teste auditivo da margem (spec-10)** — pendente do usuário: ouvir os 4 Shorts do
   sermão `mg83gcM4ctw` e confirmar que (a) a fala seguinte não vaza e (b) a margem 0,4 s
   não corta a última sílaba. Já apareceu um caso suspeito: o Short de 36 s cortou em "O
   que Deus em Cristo" (frase incompleta) — investigar se a margem 0,4 s comeu a frase;
   se sim, testar `-margem-fim 0.3`.

2. **Legenda** (o maior problema visual, ainda SEM spec). No SRT do short_01 confirmou-se:
   legenda "rolling" de 4 linhas cobrindo o rosto; texto repetido/acumulado; ">>" (troca
   de locutor) aparecendo na tela; duplicações ("superst superstição"); blocos-fantasma de
   10 ms. Solução provável: o render deve queimar o texto JÁ desduplicado/limpo (reusar o
   `Frasear` da Fase 3) em blocos legíveis (1–2 linhas no rodapé), em vez do SRT bruto.
   PRÓXIMO: escrever a spec da legenda (pedir ao usuário um frame de um Short para desenhar
   posição/tamanho/estilo).

## PRIORIDADE MÉDIA (auditoria e consistência)

3. **Funil automático** — resumo ao fim de cada seleção mostrando o caminho: "mapa N
   blocos → Fase 2 propôs X → Fase 3 Y viáveis (Z descartados, motivo) → Fase 4 aprovados/
   marcados → Fase 5 finais". Hoje só dá para auditar rodando `-ate 5` e lendo à mão.
   Responde de forma automática a pergunta recorrente "por que só achou N candidatos?".

4. **Inconsistência selecionar (1) vs harness -ate 5 (3/5)** — os dois comandos reportam
   números diferentes de candidatos para o mesmo sermão. Descobrir por quê (o `selecionar`
   provavelmente filtra por score mínimo; o `harness` não) e alinhar.

## PRIORIDADE (fluxo do operador — grande, redesenho)

5. **spec-05 — interface web do operador**, a ser REDESENHADA com a ideia de inverter a
   ordem (registrada em `ideias-futuras.md`): baixar só a legenda → selecionar → operador
   pré-visualiza os trechos pelo player do YouTube (start/end) e aprova/ajusta → só então
   baixa o vídeo e corta local. Inclui o ajuste editorial fino do corte pelo operador
   (nota já registrada na spec-05).

6. **spec-06 — retenção e limpeza de disco** (apagar `video.mp4`/temporários após
   entregar). Menor; pode vir por último.

## NOTAS TRANSVERSAIS

- Baseline do modelo Gemma (para comparar se testar Qwen um dia): retry ~5%, sempre Fase 1
  "blocos vazio", núcleo estável após ajuste de prompt. Em `avaliacao-de-modelo-e-prompt.md`.
- Princípio consolidado: decisão objetiva/mensurável → código automático; julgamento
  subjetivo (fidelidade) → humano assistido por marcação do modelo.
