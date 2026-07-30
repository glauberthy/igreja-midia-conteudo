# Overshoot da parada da escuta

Mede quanto a reprodução passa do ponto pedido — o erro que fazia o operador ouvir mais do que o
Short conteria e calibrar o corte num ponto já curto.

```bash
cp finalizados/<pedido>/short_01.mp4 docs/medicoes/overshoot-parada/v.mp4   # qualquer mp4 local
python3 -m http.server 7860 -d docs/medicoes/overshoot-parada
# abra http://localhost:7860/ e clique "medir 10 pontos"
```

**Precisa do navegador do dono** (regra 7 do CLAUDE.md): o `requestAnimationFrame` é conduzido
pelo compositor e **não tica em Chrome headless sem display** — verificado em 2026-07-30, com
`--headless=new` e com o headless legado, inclusive com `--run-all-compositor-stages-before-draw`.
A página não roda ali, então o número tem de vir do navegador de verdade.

Medições já feitas, no player do YouTube (essas SIM rodaram em headless, porque a parada era por
`setInterval`):

| mecanismo | overshoot, mediana | faixa |
|---|---|---|
| poll de 200 ms, sem folga (até 2026-07-30) | **+89 ms** | +77 a +101 |
| poll de 40 ms, folga de 40 ms para dentro | **−22 ms** | −37 a +3 |
| `requestAnimationFrame` sobre `<video>` local (v4) | **a medir** | esperado: ±1 quadro |

O `v.mp4` não é versionado (`.gitignore`): é qualquer arquivo local, porque o que se mede é o
MECANISMO de parada, não o conteúdo.
