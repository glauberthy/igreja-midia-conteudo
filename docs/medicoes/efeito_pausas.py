"""Recalcula os DOIS números que medem o efeito do encaixe em pausa, direto do cortes.csv.

A previsão registrada em docs/medicoes/pausas-de-fala.md, com a linha de base de campo (37 cortes
até 2026-07-29):

    proporção de trechos que precisaram de ajuste .... 27%
    mediana do delta do FIM, entre os que mexeram .... +2250 ms

Com a fronteira do corte vindo do ÁUDIO, o corte já nasce onde a fala termina, então os dois devem
ENCOLHER. Se não encolherem, a hipótese está errada — e este comando é o que diz isso, em vez de
opinião.

Uso:

    python3 docs/medicoes/efeito_pausas.py                      # antes x depois de 2026-07-30
    python3 docs/medicoes/efeito_pausas.py -corte 2026-08-15    # outra data de corte
    python3 docs/medicoes/efeito_pausas.py -csv outro.csv

Por que não é um comando Go: é leitura de CSV para o dono conferir uma previsão, no mesmo lugar e
no mesmo estilo dos outros scripts de medição (medir_deslocamento_srt.py, medir_bordas.py). Não
entra no caminho do operador.
"""

import argparse
import csv
import statistics as st

# A data em que o encaixe em pausa entrou (v4, fatia 1).
CORTE_PADRAO = "2026-07-30"


def resumo(rows, rotulo):
    if not rows:
        print(f"{rotulo}: nenhuma linha")
        return
    ajustados = [r for r in rows if r["ajustado"] == "sim"]
    mexeram_fim = [int(r["delta_end_ms"]) for r in rows if int(r["delta_end_ms"]) != 0]
    prop = 100 * len(ajustados) / len(rows)
    print(f"\n{rotulo}   ({len(rows)} cortes aprovados)")
    print(f"  precisaram de ajuste ......... {len(ajustados):>3} de {len(rows):<3} = {prop:.0f}%")
    if mexeram_fim:
        m = st.median(sorted(mexeram_fim))
        print(f"  delta do FIM (quem mexeu) ... n={len(mexeram_fim):<3} mediana {m:+.0f} ms"
              f"   p25 {sorted(mexeram_fim)[len(mexeram_fim)//4]:+d}"
              f"   p75 {sorted(mexeram_fim)[3*len(mexeram_fim)//4]:+d}")
    else:
        print("  delta do FIM (quem mexeu) ... ninguém mexeu no fim")
    # A regra que decidiu o fim não está no cortes.csv (ela vive no acoes.csv); aqui só o efeito.
    return prop, (st.median(sorted(mexeram_fim)) if mexeram_fim else 0)


def uma_por_trecho(rows):
    """Mantém a ÚLTIMA linha de cada (pedido, indice).

    Reaprovar o mesmo trecho é iteração, não amostra nova: numa sessão de depuração o mesmo corte
    entra dez vezes e afogaria a estatística com um único caso. O cortes.csv guarda todas de
    propósito (é registro), e a análise é que escolhe — mesma disciplina de amostra que levou o
    arquivo a registrar TODOS os aprovados, e não só os ajustados.
    """
    ultima = {}
    for r in rows:
        ultima[(r["pedido"], r["indice"])] = r  # o CSV está em ordem cronológica
    return list(ultima.values())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-csv", default="resultados/cortes.csv")
    ap.add_argument("-corte", default=CORTE_PADRAO,
                    help="data em que o encaixe em pausa entrou (AAAA-MM-DD)")
    ap.add_argument("-todas", action="store_true",
                    help="não deduplica: conta toda linha, inclusive reaprovações do mesmo trecho")
    a = ap.parse_args()

    rows = list(csv.DictReader(open(a.csv, encoding="utf-8")))
    brutas = len(rows)
    if not a.todas:
        rows = uma_por_trecho(rows)
        print(f"amostra: {len(rows)} trechos distintos (de {brutas} linhas; reaprovações do mesmo "
              f"trecho contam uma vez — use -todas para ver tudo)")
    antes = [r for r in rows if r["quando"] < a.corte]
    depois = [r for r in rows if r["quando"] >= a.corte]

    print(f"corte em {a.corte}")
    ra = resumo(antes, "ANTES do encaixe em pausa (linha de base)")
    rd = resumo(depois, "DEPOIS do encaixe em pausa")

    if ra and rd:
        print("\nveredito da previsão:")
        for nome, va, vd, unidade in (("proporção de ajustados", ra[0], rd[0], "%"),
                                      ("mediana do delta do fim", ra[1], rd[1], " ms")):
            if vd == 0 and va == 0:
                print(f"  {nome}: sem dado suficiente")
                continue
            seta = "ENCOLHEU" if abs(vd) < abs(va) else ("igual" if vd == va else "CRESCEU")
            print(f"  {nome}: {va:.0f}{unidade} -> {vd:.0f}{unidade}   {seta}")
        print("\nCUIDADO ao ler com pouca amostra: os deltas são quantizados por fronteira de fala,")
        print("então a mediana salta entre valores discretos. Olhe a forma, não só o centro — e")
        print("desconsidere as linhas de depuração (várias aprovações do MESMO trecho no mesmo dia).")


if __name__ == "__main__":
    main()
