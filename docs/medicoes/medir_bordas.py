"""Mede AS BORDAS de um Short: onde o operador pediu, onde o corte caiu, e onde a fala
realmente começa e termina.

Existe porque "o corte não pegou igual eu escuto" tem pelo menos três causas possíveis, e
elas erram em direções diferentes — sem separá-las, consertar é chute:

  1. PERDA NO CAMINHO  o ajuste do operador vem em ms, mas o candidato guarda "HH:MM:SS.mmm";
                       se alguém truncar o milissegundo, o render corta antes do pedido.
  2. ESCUTA vs PRODUTO o operador ajusta de ouvido no player do YouTube, com a parada feita
                       por um timer em JS; o Short é cortado pelo ffmpeg. Se o timer para
                       depois do ponto, ele OUVE áudio que o arquivo não terá.
  3. LEGENDA vs ÁUDIO  o carimbo da legenda adianta a fala (spec-12/Rota D). Isso desloca o
                       ponto de partida do operador, não o corte em si.

O que este script faz, para UM trecho:

  * lê a duração real do arquivo entregue (ffprobe) — a verdade do produto;
  * mede o nível de áudio nos 300 ms do começo e do fim do arquivo — se está alto, o corte
    caiu NO MEIO da fala, que é o sintoma que o operador ouve;
  * acha, na FONTE, as fronteiras de fala em volta dos tempos pedidos (silencedetect) — a
    verdade do áudio, sem legenda e sem modelo no meio;
  * compara tudo e diz, em milissegundos, quanto cada camada errou.

Uso:

  python3 docs/medicoes/medir_bordas.py \\
      --short finalizados/<pedido>/short_01.mp4 \\
      --fonte videos/<idVideo>/video.mp4 \\
      --pediu 3997000,4028250 \\
      [--candidato 01:06:37.000,01:07:08.000]

`--pediu` são os ms que o operador enviou (o `ajuste_N` do POST /aprovar).
`--candidato` são as strings gravadas no candidatos.corrigido.json, se quiser ver a perda de
formatação isolada.
"""

import argparse
import re
import subprocess
import sys

LIMIAR_SILENCIO = "-32dB"  # o mesmo limiar nos dois lados, senão a comparação não vale
DUR_SILENCIO = "0.15"      # 150 ms: pausa entre palavras não conta como fim de fala
JANELA = 4.0               # segundos de fonte examinados em volta de cada borda


def rodar(args):
    p = subprocess.run(args, capture_output=True, text=True)
    return p.stdout + p.stderr


def duracao(path):
    saida = rodar(["ffprobe", "-v", "error", "-show_entries", "format=duration",
                   "-of", "default=noprint_wrappers=1:nokey=1", path]).strip()
    try:
        return float(saida)
    except ValueError:
        sys.exit(f"não li a duração de {path}: {saida!r}")


def nivel(path, inicio_s, dur_s):
    """max_volume em dB numa fatia. Fala fica em torno de -12 dB; silêncio, abaixo de -40."""
    saida = rodar(["ffmpeg", "-ss", f"{inicio_s:.3f}", "-t", f"{dur_s:.3f}", "-i", path,
                   "-vn", "-af", "volumedetect", "-f", "null", "-"])
    m = re.search(r"max_volume:\s*(-?[\d.]+) dB", saida)
    return float(m.group(1)) if m else None


def fronteiras(path, centro_ms):
    """Transições fala<->silêncio na FONTE, em ms absolutos, em volta de centro_ms."""
    ini = max(0.0, centro_ms / 1000 - JANELA)
    saida = rodar(["ffmpeg", "-ss", f"{ini:.3f}", "-t", f"{2*JANELA:.3f}", "-i", path, "-vn",
                   "-af", f"silencedetect=noise={LIMIAR_SILENCIO}:d={DUR_SILENCIO}",
                   "-f", "null", "-"])
    eventos = []
    for m in re.finditer(r"silence_(start|end):\s*([\d.]+)", saida):
        ms = int(round(ini * 1000 + float(m.group(2)) * 1000))
        # silence_start = a fala PAROU ali; silence_end = a fala VOLTOU ali
        eventos.append(("fim_de_fala" if m.group(1) == "start" else "inicio_de_fala", ms))
    return sorted(eventos, key=lambda e: e[1])


def mais_proximo(eventos, tipo, ms):
    cands = [e for e in eventos if e[0] == tipo]
    if not cands:
        return None
    return min(cands, key=lambda e: abs(e[1] - ms))[1]


def hms(ms):
    return f"{ms//3600000:02d}:{(ms//60000)%60:02d}:{(ms//1000)%60:02d}.{ms%1000:03d}"


def par_ms(txt):
    a, b = txt.split(",")
    return int(a), int(b)


def par_hms(txt):
    def um(s):
        s = s.strip()
        frac = 0
        if "." in s:
            s, f = s.split(".")
            frac = int((f + "000")[:3])
        h, m, seg = (int(x) for x in s.split(":"))
        return ((h * 60 + m) * 60 + seg) * 1000 + frac
    a, b = txt.split(",")
    return um(a), um(b)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--short", required=True)
    ap.add_argument("--fonte", required=True)
    ap.add_argument("--pediu", required=True, type=par_ms, help="startMs,endMs do ajuste")
    ap.add_argument("--candidato", type=par_hms, help="start,end como strings do candidato")
    a = ap.parse_args()

    pediu_ini, pediu_fim = a.pediu
    print(f"PEDIDO (ajuste do operador)   {hms(pediu_ini)} -> {hms(pediu_fim)}"
          f"   dur {(pediu_fim-pediu_ini)/1000:.3f}s")

    if a.candidato:
        ci, cf = a.candidato
        print(f"CANDIDATO (string no disco)   {hms(ci)} -> {hms(cf)}"
              f"   dur {(cf-ci)/1000:.3f}s")
        if (ci, cf) != (pediu_ini, pediu_fim):
            print(f"  !! PERDA DE FORMATAÇÃO: início {ci-pediu_ini:+d} ms, fim {cf-pediu_fim:+d} ms")

    dur = duracao(a.short)
    print(f"ARQUIVO ENTREGUE              duração {dur:.3f}s"
          f"   ({(dur*1000 - (pediu_fim-pediu_ini)):+.0f} ms vs o pedido)")

    # O sintoma perceptível: o arquivo começa/termina no meio da fala?
    n_ini = nivel(a.short, 0.0, 0.3)
    n_fim = nivel(a.short, max(0.0, dur - 0.3), 0.3)
    # Rótulos em FAIXAS, e nunca como veredito: uma cauda de palavra decaindo mede -22 dB e
    # NÃO é palavra cortada. Quem decide a borda é a comparação com a fonte, adiante. Um rótulo
    # que afirma mais do que mede ensina o leitor a ignorar rótulos.
    def faixa(db):
        if db is None:
            return "?"
        if db > -20:
            return "fala em nível cheio"
        if db > -30:
            return "cauda de fala ou fala fraca"
        if db > -45:
            return "quase silêncio"
        return "silêncio"
    print(f"  primeiros 300 ms: max {n_ini:+.1f} dB -> {faixa(n_ini)}")
    print(f"  últimos   300 ms: max {n_fim:+.1f} dB -> {faixa(n_fim)}")
    print("  (nível é indício; o veredito da borda é a comparação com a fonte, abaixo)")

    print(f"\nFALA REAL NA FONTE ({LIMIAR_SILENCIO}, pausa >= {DUR_SILENCIO}s)")
    ev_i = fronteiras(a.fonte, pediu_ini)
    ev_f = fronteiras(a.fonte, pediu_fim)
    onset = mais_proximo(ev_i, "inicio_de_fala", pediu_ini)
    offset = mais_proximo(ev_f, "fim_de_fala", pediu_fim)
    if onset is not None:
        print(f"  fala começa em {hms(onset)}  ({onset-pediu_ini:+d} ms do início pedido)")
    if offset is not None:
        print(f"  fala termina em {hms(offset)} ({offset-pediu_fim:+d} ms do fim pedido)")
        if a.candidato:
            print(f"  fim EFETIVO do corte: {hms(a.candidato[1])}"
                  f" -> {a.candidato[1]-offset:+d} ms em relação ao fim da fala"
                  f"  {'(CORTA a palavra)' if a.candidato[1] < offset else '(deixa a palavra inteira)'}")
    print("\nnota: as fronteiras vêm do ÁUDIO, não da legenda — é a única referência que não\n"
          "      depende do carimbo adiantado do SRT.")


if __name__ == "__main__":
    main()
