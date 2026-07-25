# Spec 08 — Rede de retry nas chamadas ao modelo

## Objetivo

Tornar o harness resiliente a respostas malformadas do modelo. Hoje, quando o modelo
devolve JSON inválido ou incompleto, a fase falha com `exit status 1` e o harness
inteiro morre. Esta spec adiciona uma rede de **rejeitar-e-refazer**: se a resposta não
for válida, o código descarta e refaz a consulta, até um limite de tentativas.

## Contexto

Modelo local (Gemma 26B A4B, quantizado) ocasionalmente devolve formato inesperado —
visto em produção: `estrutura` como lista de objetos em vez de lista de strings, e 2 de
4 execuções da Fase 1 quebrando por isso. A tolerância pontual (parse defensivo de um
campo) resolve caso a caso, mas não é uma estratégia geral. A decisão do dono do
projeto é ter uma rede de retry que proteja **todas** as chamadas ao modelo.

Princípio: retry cobre **formato/estrutura inválidos**, NUNCA conteúdo de baixa
qualidade. Qualidade de trecho é decisão das Fases 4 e 5; o retry não tenta "melhorar"
um trecho ruim refazendo a consulta.

## Escopo

Dentro:
- Camada de retry centralizada no ponto que fala com o modelo (`ClienteLLM` /
  `ModeloLLM` em `internal/harness`). Uma única implementação; todas as fases (1, 2, 4)
  se beneficiam sem duplicar código.
- Cada fase informa a **validação** da própria resposta (quais campos são obrigatórios).
- Limite de tentativas e erro claro ao esgotar.
- Log de cada tentativa falha (visível ao operador).

Fora:
- Não altera a lógica de seleção/avaliação das fases (só como a resposta é obtida e
  validada).
- Não trata qualidade de conteúdo (isso é das Fases 4 e 5).

## Decisões já tomadas (não reabrir)

- Retry centralizado na camada do modelo, não espalhado nas fases.
- Máximo de **3 tentativas** por chamada. Esgotou as 3 → erro claro (nº de tentativas +
  último motivo).
- "Falha que merece retry": (a) erro de transporte/rede; (b) resposta não é JSON válido;
  (c) JSON válido mas faltam campos obrigatórios da chamada.
- Retry NÃO se aplica a conteúdo ruim — só a formato/estrutura inválidos.
- Cada tentativa falha é logada (ex.: "Fase 2: tentativa 1 falhou (JSON inválido),
  refazendo…").

## Contrato

Uma função central, algo como:

```
PedirValidado(ctx, prompt, valida func([]byte) error) ([]byte, error)
```

- Chama o modelo; aplica `valida` ao corpo da resposta.
- Se `valida` retorna erro OU a chamada falha (rede/JSON), repete, até 3 tentativas.
- Sucesso: retorna a resposta válida.
- Falha nas 3: retorna erro com contagem de tentativas e o último motivo.

Cada fase passa sua própria `valida`:
- Fase 1: JSON parseável em `Mapa` com `tema_central` não vazio e ao menos 1 bloco.
- Fase 2: JSON com `candidatos`, cada um com `bloco` e `frase_ancora` não vazios.
- Fase 4: JSON com o objeto `criteria` contendo os 5 campos numéricos.

## Questões em aberto (decidir na execução, registrar)

- Pequeno backoff entre tentativas? Provavelmente desnecessário para modelo local (não
  há rate limit); manter simples (sem espera) salvo se surgir motivo.
- Reaproveitar o parse defensivo de `estrutura` (spec anterior) — manter; ele e o retry
  são complementares (um tolera campo decorativo, o outro protege campos essenciais).

## Critérios de aceite

- [ ] Retry centralizado na camada do modelo; fases 1, 2 e 4 usam a mesma função.
- [ ] Cada fase informa sua validação de campos obrigatórios.
- [ ] Máximo 3 tentativas; ao esgotar, erro claro com contagem e último motivo.
- [ ] Retry cobre rede + JSON inválido + campos faltando; NÃO cobre conteúdo ruim.
- [ ] Cada tentativa falha é logada de forma visível.
- [ ] Testes (fake): resposta inválida seguida de válida → sucesso na 2ª tentativa;
      três inválidas → erro após 3 tentativas; resposta válida na 1ª → sem retry.
- [ ] `go build ./...` e `go test ./...` verdes.
- [ ] Teste real: harness completo 4x no sermão `mg83gcM4ctw` sem quebrar; se houver
      retry, aparece no log. Registrar com que frequência o retry disparou (é um
      medidor da confiabilidade do modelo no formato).

## Como validar

```bash
go test ./...
for i in 1 2 3 4; do
  echo "== rodada $i =="
  go run ./cmd/harness -transc trabalho/sermao/transcricao.txt -ate 5 \
    -out-final trabalho/sermao/finais_$i.json
done
# observar no log: quantas vezes "tentativa N falhou" apareceu.
```

## Nota estratégica

O log de retry vira um **medidor da confiabilidade do modelo**: se o retry dispara
raramente, o modelo é confiável no formato; se dispara com frequência, é evidência a
favor de trocar de modelo (Qwen), decisão ainda em aberto no projeto.

---

## Adendo — robustez das chamadas ao modelo (além do retry)

> Registrado depois do fato (dívida documental do inventário de specs). Com estas duas
> defesas, o escopo real da spec-08 passou a ser **"robustez das chamadas ao modelo"**,
> não só o retry. Ambas são **genéricas** — valem para qualquer modelo, não só o Qwen —
> e ambas se integram ao `PedirValidado`/`ClienteLLM` (a mesma camada do retry).

### (a) Descascar JSON embrulhado antes do parse — commit `c7525ee`

Alguns modelos (ex.: Qwen) embrulham o JSON em cerca de código markdown (```` ```json ````
… ```` ``` ````) ou o cercam de texto explicativo. O parse tropeçava (ex.: `invalid
character '`'`). `PedirValidado` passou a **descascar** a resposta antes de validar/parsear,
em TODAS as fases que falam com o modelo (1, 2 e 4). `descascarJSON` é idempotente: remove
a cerca (e o rótulo de linguagem), recorta o contêiner externo (`{…}` ou `[…]`) e descarta
texto antes/depois. **JSON puro (Gemma) sai intacto.**

### (b) Detectar resposta truncada por `max_tokens` — commit `31a2c37`

Quando o modelo estoura o `max_tokens`, o JSON vem cortado no meio e o parse acusava o
críptico `unexpected end of JSON input`. Agora, `finish_reason=length` vira um **erro
claro e RETRYÁVEL** (`resposta truncada em max_tokens=N (finish_reason=length)`) — a
próxima tentativa costuma sair completa; se persistir, o operador sabe que é o limite, não
um bug. Nunca deixa passar JSON incompleto como se fosse válido. Junto, a folga de tokens
das Fases 1 e 2 subiu para 4096 (uma resposta típica da Fase 2 tem ~700 tokens; a margem
maior reduz o truncamento).

Nota: o truncamento crônico da Fase 2 com o Qwen Q3 foi resolvido de fato pela
desduplicação da transcrição (ver spec-15), que corta o input em ~68%. Estas defesas são a
rede de segurança que torna o erro **claro e recuperável** quando ainda acontecer.

### Critérios de aceite (adendo)

- [x] Resposta em cerca markdown ou cercada de texto parseia (JSON puro passa intacto).
- [x] `finish_reason=length` → erro "truncada", retryável; nunca passa JSON incompleto.
- [x] Defesas genéricas (qualquer modelo), na mesma camada do retry.
- [x] Testes: `TestDescascarJSON`, `TestPedirValidadoDescasca*` (harness), `TestClienteLLMTruncada`.
