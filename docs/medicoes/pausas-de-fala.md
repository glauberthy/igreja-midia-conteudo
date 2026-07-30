# Pausas de fala: parâmetros medidos e linha de base do efeito

Medições de 2026-07-30, culto `fZGyLBofmmo` (1h50, 902 MB). Reproduzíveis com
`go run ./docs/medicoes/pausas -id <idVídeo>` e `docs/medicoes/medir_bordas.py`.

## Por que trocar a fronteira do corte

O corte terminava na fronteira do **bloco de legenda** seguinte. Blocos da legenda *rolling*
quebram por largura de tela, não por fim de fala:

```
transcrição: [01:30:05] "capaz de arrumar o caos. O Senhor traga"
             [01:30:07] "ordem na minha vida. Traga vida para…"
áudio:        fala corrida de 01:30:04.126 até 01:30:09.486
```

O operador clicou na frase, o corte terminou em **01:30:06** — 3,5 s antes de a fala parar — e
todos os deltas do histórico de ações deram **zero**: o sistema aplicou fielmente um ponto que a
legenda inventou.

## Custo da detecção

| medida | valor |
|---|---|
| tempo para 1h50 de áudio | **6,5–7,2 s** (praticamente independente dos parâmetros: domina decodificar o áudio) |
| pausas (−32 dB, ≥300 ms) | **1835** |
| tamanho do `pausas.json` | ~80 KB |
| quando roda | uma vez por culto, guardado no cache ao lado do vídeo |

## Escolha dos parâmetros

### Duração mínima: o critério foi um caso conhecido

Pergunta: qual limiar **não inventa** pausa dentro de uma fala corrida? Medido na fala de 5,4 s
acima (01:30:04.126 → 01:30:09.486):

| `d` | pausas espúrias dentro da fala |
|---|---|
| 0,10 s | **cinco** (136, 152, 100, 101, 108 ms) — micro-vãos entre palavras. Uma delas em 5406651, quase exatamente onde o corte errado caiu |
| 0,15 s | **uma** (152 ms), logo após o início da frase |
| 0,20 s | nenhuma |
| **0,30 s (escolhido)** | nenhuma, com margem |

### Limiar em dB: quase não importa

Culto inteiro, `d=0,15`: −30 dB → 2940 pausas; −32 → 2736; −35 → 2472; −40 → 2026. A sala é
silenciosa e a fala é alta, então a escolha cai numa região plana. **−32 dB** fica no meio dela.

### O que estes parâmetros NÃO fazem

**Não distinguem "respirou" de "terminou a frase".** A distribuição das 2736 pausas é
**unimodal**, sem vale entre modas:

```
0,15–0,25 s  738  ################
0,25–0,40 s  443  #########
0,40–0,60 s  498  ##########
0,60–0,90 s  430  #########
0,90–1,50 s  369  ########
1,50–3,00 s  197  ####
   > 3,00 s   61  #
mediana 0,47 s · p25 0,24 · p75 0,85 · p90 1,46
```

E há contraexemplos diretos: **467 ms** é fim de sentença ("…arrumar o caos.") e **934 ms** é meio
de sentença ("…de tantas vozes que nunca me | trouxeram nada…"). Ou seja: a duração é **pista de
ordenação**, nunca classificador. O limiar remove micro-vão entre palavras; escolher entre pausas
candidatas continua sendo do operador — e é por isso que a régua (fatia 3) vai desenhá-las.

Configurável por `-pausa-db` e `-pausa-min-ms` no `cmd/servidor`, e os valores usados são
**gravados** no `pausas.json`: análise de receita diferente da configurada é ignorada e refeita.

## Consistência entre a onda e as pausas (quando a régua chegar)

As duas saem do **mesmo arquivo** (`videos/<id>/video.mp4`) e do mesmo áudio decodificado pelo
ffmpeg; a onda (`showwavespic`) é a envoltória crua, sem limiar, então não pode "discordar" sobre
onde há energia. O que garante o casamento na tela é: (a) mesma janela de tempo, (b) mesma
conversão ms→px, (c) as marcas de pausa desenhadas **sobre** a onda, e (d) o `pausas.json`
carregando a receita, para a régua nunca desenhar marcas de uma análise e o encaixe usar outra.

## Efeito no caso real (antes e depois)

```
clique em frase, fim pedido 5405000 (fronteira do bloco de legenda)

ANTES   fim = 5405000   regra = legenda   dur 44,00 s
        o arquivo terminava com fala em nível cheio (-17,5 dB), 3,5 s antes da pausa

DEPOIS  fim = 5409486   regra = pausa     dur 48,49 s   deslocamento +4486 ms
        o arquivo termina onde a fala para; perfil dos últimos 600 ms mostra a
        palavra fechando (-12,1 -> -25,0 dB) em vez de ser cortada
```

O empurrão fino continua valendo exatamente o pedido (5405250 → 5405250, `regra = pedido`): as
duas regras são diferentes de propósito.

## LINHA DE BASE — a previsão observável

O `cortes.csv` já coleta original × final por trecho aprovado. **Antes** do encaixe em pausa
(37 linhas, até 2026-07-29):

| medida | valor |
|---|---|
| trechos que precisaram de ajuste | **27%** (10 de 37) |
| delta do FIM, mediana de todos | +0 ms |
| delta do FIM, mediana dos que mexeram no fim | **+2250 ms** |
| delta do INÍCIO, mediana | +0 ms |

Aquele **+2250 ms** é a assinatura do problema: o operador empurrava o fim ~2,25 s para frente
porque a fronteira da legenda caía cedo.

**Previsão falsificável:** com o encaixe em pausa, o corte já nasce onde a fala termina, então
(a) a proporção de ajustados deve **cair** de 27%, e (b) a mediana do delta do fim dos que ainda
ajustarem deve **encolher** — o que sobrar é preferência do operador, não conserto de fronteira.

Se **não** encolher, a hipótese está errada e o dado dirá isso: será sinal de que o desvio é da
legenda em outro eixo (e a Rota D volta ao centro). Medir com `resultados/cortes.csv` filtrando
`quando >= 2026-07-30`.
