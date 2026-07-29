# Medição: de onde vem a legenda adiantada e o corte cedo

> Rodar de novo: `python3 docs/medicoes/medir_deslocamento_srt.py <arquivo.srt> [...]`

## O diagnóstico antigo estava errado

Registramos que "a legenda do YouTube é dessincronizada". **Não é.** O operador verificou
assistindo no YouTube: a legenda está sincronizada. O defeito é da **nossa extração**.

Cada bloco do SRT tem início e fim e fica na tela durante a janela inteira. O
`internal/transcricao` usa `parseStartMs` — só o início — e junta todas as linhas do bloco
naquele único instante (`transcricao.go:69` e `:87`). O fim é lido e **descartado**.

Como o texto novo aparece sempre por último no bloco, toda palavra nova recebe o instante em
que o bloco *apareceu*, e não o instante em que foi *dita*.

## Números (4 sermões reais)

| SRT | formato | blocos de conteúdo | janela real da fala | `fim` do bloco |
|---|---|---|---|---|
| `legenda.srt` (Joice, 2026-07-26) | duas linhas + transições de 10 ms | 1868 | **3,10 s** | 3,10 s |
| `transcricao_1.pt.srt` | uma linha, blocos sobrepostos | 712 | **3,10 s** | 6,10 s |
| `transcricao_3.pt.srt` | uma linha, blocos sobrepostos | 616 | **3,38 s** | 6,73 s |
| `transcricao_5.pt.srt` | uma linha, blocos sobrepostos | 920 | **2,50 s** | 4,94 s |

**Erro que estamos corrigindo**, medido:

- **última palavra do texto novo: 2,5 a 3,4 s adiantada** (média ~3,0 s);
- palavra do meio: ~1,3 a 1,7 s adiantada.

Isso confirma quantitativamente o exemplo do dono (bloco `01:22:33,960 --> 01:22:36,350`,
`"nenhum paga."` recebendo 22:33 quando "paga" sai perto de 22:36 — erro de ~2,4 s).

E explica os **dois sintomas com uma causa só**: a legenda queimada troca cedo porque o
carimbo da linha está adiantado; o corte fecha cedo porque o `FimMs` da frase é o carimbo da
última linha, não o instante da última palavra.

## O que a medição mudou no plano

O plano dizia "interpolar usando o tempo de fim que hoje é descartado". A medição mostra que
**o `fim` do bloco só é confiável num dos dois formatos**:

- **duas linhas + transições** (legendas atuais): `fim - ini` = 3,10 s = a janela real. Serve.
- **uma linha, blocos sobrepostos** (legendas mais antigas): `fim - ini` = 6,10 s contra 3,10 s
  reais — **o dobro**. Ali o bloco continua na tela depois de a fala acabar, porque a linha
  sobe e coexiste com a seguinte.

Interpolar em `[ini, fim]` no formato antigo jogaria a última palavra ~6 s para frente: **erro
maior que o atual, e na direção oposta**. A janela correta, que vale nos dois formatos, é:

```
[ini, min(fim_do_bloco, ini_do_próximo_bloco_de_conteúdo))
```

Note como as duas colunas "janela real" convergem em 2,5–3,4 s nos quatro arquivos, apesar de
o `fim` variar de 3,1 a 6,7 s. É a janela, não o `fim`, que é a grandeza estável.
