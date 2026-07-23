# Runbook — Executar o pipeline (baixar → selecionar → validar → renderizar)

Como rodar o pipeline completo à mão, do link do YouTube até os Shorts em
`finalizados/<id>/`. Rode tudo a partir da raiz do projeto (`~/Desktop/shorts_igreja`).

Visão geral das etapas:

1. **baixar** — legenda automática pt + vídeo do trecho da pregação (`cmd/baixar`, usa `yt-dlp`).
2. **selecionar** — harness multifase (`cmd/selecionar`, usa o `llama-server`): o modelo mapeia o sermão, escolhe candidatos e os avalia; o código delimita o tempo (30–58 s) e valida. A validação e a rede de retry já rodam embutidas.
3. **renderizar** — corta, reenquadra 9:16 e queima a legenda de cada candidato (`cmd/render`, usa `ffmpeg`).

---

## 1. Pré-requisitos (uma vez por sessão)

### a) yt-dlp funcional

O `yt-dlp` do sistema pode estar quebrado (se estiver rodando sob Python < 3.10).
Baixe o binário standalone (traz o próprio Python):

```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux -o ~/.local/bin/yt-dlp
chmod +x ~/.local/bin/yt-dlp
~/.local/bin/yt-dlp --version   # confirma que funciona
```

### b) ffmpeg

Necessário para o download da seção, conversão de legenda e a renderização.
`ffmpeg -version` deve responder. Instalar: `sudo apt install ffmpeg`.

### c) llama-server no ar (modelo Gemma)

É o `~/start-gemma.sh`, **com `--parallel 1` adicionado**. Isso é essencial: sem
`--parallel 1`, o padrão divide o contexto de 64k em 4 slots (16k cada) e o prompt
de um sermão (~35k tokens) não cabe.

```bash
~/llama.cpp/build/bin/llama-server \
  -m  ~/models/gemma-4-26B-A4B-it-qat-UD-Q4_K_XL.gguf \
  -md ~/models/gemma-4-26b-A4B-it-assistant-Q4_0.gguf \
  --spec-type draft-mtp --spec-draft-n-max 6 --spec-draft-p-min 0.7 \
  -c 64000 -ngl 99 -ngld 99 --flash-attn on \
  --cache-type-k q8_0 --cache-type-v q8_0 \
  --parallel 1 --host 127.0.0.1 --port 8080 --jinja
```

Deixe rodando num terminal; espere a linha `server is listening`. Verifique:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/health   # deve dar 200
```

Notas de infra (validado numa RTX 4000 Ada, 20 GB VRAM):
- `--flash-attn on` + `--cache-type-k/v q8_0` são o que faz os 64k de contexto
  caberem na VRAM junto com o modelo de 14 GB. Sem o KV quantizado, dá OOM.
- O modelo *draft* (`-md ...assistant-Q4_0.gguf`) acelera a geração (speculative decoding).

---

## 2. Pipeline (por sermão)

Substitua `-url`, `-inicio`, `-fim` e o `-id` para cada pregação. **Use um `-id`
próprio por sermão** (ex.: `-id sermao-<slug>`): cada `id` fica isolado em
`trabalho/<id>/` e `finalizados/<id>/`. Se você reutilizar um `id` que já aponta para
OUTRO vídeo/janela, o `baixar` **recusa** com um aviso (para nunca misturar o vídeo de
um pedido com a transcrição de outro) — passe `-force` para substituir de propósito
(apaga os artefatos daquele id e rebaixa do zero).

Defina o `id` uma vez numa variável de shell e reaproveite nos três comandos — assim
você não repete (nem erra) o id em cada linha:

```bash
cd ~/Desktop/shorts_igreja

# Um id por sermão (troque a cada pregação):
ID=culto-noite-19-07-26

# (1) BAIXAR — legenda original pt + vídeo do trecho da pregação, em 1080p.
go run ./cmd/baixar \
  -url "https://www.youtube.com/watch?v=xZNTJcehAV0" \
  -inicio 00:49:15 -fim 01:24:30 -id "$ID" \
  -bin ~/.local/bin/yt-dlp -sublang pt-orig \
  -format "bv*[height<=1080]+ba/b[height<=1080]/b"

# (2) SELECIONAR — harness multifase (Fases 1→5); validação embutida.
# O modelo mapeia, escolhe candidatos e avalia; o código delimita tempo e valida.
# O endpoint tem default http://localhost:8080/v1/chat/completions (só passe -endpoint
# se mudar a porta). É normal ver "tentativa N falhou … refazendo" no log (rede de retry).
go run ./cmd/selecionar \
  -transc "trabalho/$ID/transcricao.txt" \
  -out    "trabalho/$ID/candidatos.corrigido.json" \
  -prompt-dir prompts/

# (3) RENDERIZAR — corta, reenquadra 9:16 e queima a legenda de cada candidato.
# Aplica margem de recuo de 0,4s no fim do corte (spec-10), para não vazar a fala
# seguinte. Se algum Short cortar a última sílaba, rode com -margem-fim 0.3.
go run ./cmd/render -id "$ID"

# resultado (em ordem de score):
ls -lh "finalizados/$ID/"            # short_01.mp4 ... short_NN.mp4
```

O `-prompt-dir prompts/` não muda entre sermões (os prompts são compartilhados). Só o
`id` (e os caminhos que o contêm) varia.

---

## 3. Conferência (opcional)

```bash
# (reutiliza o mesmo $ID definido na seção 2)

# validador standalone sobre o corrigido (deve reportar "nenhum problema"):
go run ./cmd/validar -json "trabalho/$ID/candidatos.corrigido.json" -transc "trabalho/$ID/transcricao.txt"

# hooks e scores, do maior para o menor (aspas duplas: o shell expande $ID):
python3 -c "import json;p='trabalho/$ID/candidatos.corrigido.json';[print(c['score'],c['start'],c['hook']) for c in sorted(json.load(open(p))['candidatos'],key=lambda x:-x['score'])]"

# dimensões (devem ser 1080x1920) e duração de cada short:
for f in "finalizados/$ID"/short_*.mp4; do
  ffprobe -v error -select_streams v -show_entries stream=width,height -of csv=p=0 "$f"
done
```

### Verificação de alinhamento (recomendada — ver spec-04)

O `video.mp4` começa em t=0 (rebaseado pelo `--download-sections`), mas os tempos da
transcrição são absolutos. O corte é feito em `start - inicio`. Para conferir que o
Short mostra a fala certa:

```bash
# duração do video.mp4 deve ser ~ (fim - inicio); start_time ~ 0
ffprobe -v error -show_entries format=start_time,duration -of default=noprint_wrappers=1 "trabalho/$ID/video.mp4"

# frame do short no início vs. frame do video.mp4 no ponto absoluto do hook:
# (ex.: hook em 01:37:05, inicio 01:29:38 -> 447s no video.mp4)
ffmpeg -y -ss 447 -i "trabalho/$ID/video.mp4" -frames:v 1 /tmp/checa_video.png
ffmpeg -y -ss 2   -i "finalizados/$ID/short_01.mp4" -frames:v 1 /tmp/checa_short.png
# abra as duas imagens: devem ser a mesma cena (o short é o crop 9:16 dela).
```

---

## Solução de problemas

| Sintoma | Causa provável | Solução |
|---|---|---|
| Passo (2) devolve vazio ou erro de conexão | `llama-server` fora do ar, ou sem `--parallel 1` (prompt não cabe em 16k/slot) | Cheque `/health`; reinicie com `--parallel 1` |
| Passo (2) devolve `content` vazio após muito tempo | `enable_thinking` não desligado | Já está no código (`chat_template_kwargs.enable_thinking=false`); garanta `--jinja` no servidor |
| Download trava numa conexão pendurada do googlevideo | conexão instável | As flags de reconexão do ffmpeg já estão no `cmd/baixar`; se persistir, tente um `-format` de menor resolução |
| `ErrSemLegenda` | vídeo sem legenda automática pt | Decisão de projeto (DP-001): o processo para. Tente `-sublang pt` se `pt-orig` não existir |
| Short com a cena de um trecho e legenda de outro | desalinhamento de tempo | Não deve ocorrer (corte em `start - inicio`); rode a verificação de alinhamento acima |

## Referências

- Parâmetros do modelo e armadilhas: `docs/aprendizados-do-spike.md`
- Alinhamento de tempo (crítico): `docs/specs/spec-04-video-9x16-legenda.md`
- Decisão de não transcrever localmente (DP-001): `docs/specs/spec-03-download-legenda.md`
