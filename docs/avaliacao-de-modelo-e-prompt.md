# Avaliação de modelo e prompt — protocolo e baseline

Este documento registra **como avaliar** uma mudança de modelo ou de prompt no harness,
e o **baseline** medido até agora. Não é código nem ferramenta — é o procedimento manual
que se repete quando algo relevante muda (novo modelo, prompt ajustado). Se um dia esse
ritual passar a ser feito com muita frequência, aí vale automatizar (spec própria); até
lá, este checklist basta.

## Por que existe

A qualidade do harness depende de duas coisas que variam: o **modelo** e os **prompts**.
Modelo local não é determinístico — a mesma entrada gera saídas diferentes entre
execuções. Para saber se uma mudança melhorou de verdade (e não por sorte de uma
rodada), é preciso rodar várias vezes e medir. "Achar que melhorou" não conta; medir conta.

## Protocolo de avaliação (rode a cada mudança relevante)

1. Sermão de referência: usar sempre o(s) mesmo(s) sermão(ões) conhecido(s) — hoje o
   `mg83gcM4ctw` (transcrição em `trabalho/sermao/transcricao.txt`). Um sermão de
   referência fixo permite comparar entre modelos/prompts.
2. Rodar o harness completo **4 vezes** seguidas (mais, se houver dúvida):
   ```bash
   for i in 1 2 3 4; do
     echo "== rodada $i =="
     go run ./cmd/harness -transc trabalho/sermao/transcricao.txt -ate 5 \
       -out-final trabalho/sermao/finais_$i.json
   done
   ```
3. Registrar quatro medidas:
   - **Estabilidade do núcleo**: os trechos doutrinariamente centrais (divindade de
     Cristo, apelo do evangelho, graça) aparecem em TODAS as rodadas? (é o que mais
     importa — o núcleo não pode ser loteria).
   - **Variância de borda**: quanto variam os candidatos secundários (aplicações,
     ilustrações) entre rodadas? Alguma variação aqui é tolerável.
   - **Estabilidade do mapa (Fase 1)**: o número de blocos oscila muito entre rodadas?
   - **Taxa de retry**: quantas vezes o retry disparou, e por quê (JSON inválido? mapa
     vazio? campo faltando?). É um medidor direto da confiabilidade do modelo no formato.
4. Conferir SEMPRE uma ou duas âncoras contra a transcrição (grep), antes de concluir.
   Número na faixa não garante sentido correto; só a conferência na fonte garante.

## Critério de "bom o suficiente"

- Núcleo estável (trechos centrais em todas as rodadas): **obrigatório**.
- Retry recuperável e raro (< ~10%): aceitável.
- Variância de borda: tolerável (o operador revisa antes de publicar).
- Mapa com número de blocos oscilando: tolerável se não afeta a escolha dos candidatos.

## Baseline medido

### Gemma 4 26B A4B (QAT UD-Q4_K_XL) — modelo atual
Data: durante o desenvolvimento do harness multifase (spec-07/08).
Após o ajuste de prompt (ordem de prioridade na Fase 2) e a rede de retry (spec-08):

- **Núcleo estável**: SIM. Em 4 rodadas, a divindade de Cristo apareceu nas 4 (sempre
  em 1º), o apelo final nas 4, a graça-confronta-legalismo em 3 de 4. O núcleo deixou
  de ser loteria (antes do ajuste, os trechos centrais variavam e a divindade sumia).
- **Variância de borda**: presente — a 4ª/5ª vaga (aplicações/esperança futura) troca
  entre rodadas. Aceitável.
- **Estabilidade do mapa**: oscila — nº de blocos entre 13 e 19 nas 4 rodadas. Não
  afetou a escolha dos candidatos. É a próxima fonte de variância se quiser apertar.
- **Taxa de retry**: ~5% (2 disparos em ~36 chamadas, 4 rodadas). TODOS na Fase 1, e
  todos por **mapa sem blocos** (não por JSON inválido) — o Gemma às vezes devolve um
  mapa vazio. Sempre recuperado na 2ª tentativa.

Leitura: o modelo é razoavelmente confiável no formato, mas não perfeito (o mapa vazio
ocasional é sua fragilidade típica). Bom o suficiente para produção com revisão humana.

### (Espaço para o próximo modelo avaliado — ex.: Qwen 14B)
Ao testar outro modelo, repetir o protocolo e preencher as mesmas 4 medidas aqui, para
comparação direta com o baseline do Gemma acima.
