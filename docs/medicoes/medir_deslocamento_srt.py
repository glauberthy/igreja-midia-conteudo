import re, sys, statistics as st

def ms(t):
    h,m,rest=t.split(':'); s,mil=rest.split(',')
    return ((int(h)*60+int(m))*60+int(s))*1000+int(mil)

def limpa(t):
    t=re.sub(r'<[^>]*>','',t); t=re.sub(r'\[[^\]]*\]','',t)
    return re.sub(r'\s+',' ',t).strip()

def blocos(txt):
    txt=txt.replace('\r\n','\n').replace('\r','\n').lstrip('﻿')
    out=[]
    for b in re.split(r'\n\s*\n', txt):
        b=b.strip()
        if not b: continue
        ls=b.split('\n')
        ti=next((i for i,l in enumerate(ls) if '-->' in l), None)
        if ti is None: continue
        a,_,c=ls[ti].partition('-->')
        try: ini,fim=ms(a.strip()), ms(c.strip().split()[0])
        except Exception: continue
        linhas=[l.strip() for l in ls[ti+1:] if l.strip() and l.strip()!='\xa0']
        out.append((ini,fim,linhas))
    return out

for path in sys.argv[1:]:
    bs=blocos(open(path,encoding='utf-8',errors='replace').read())
    # blocos de CONTEÚDO: os de transição (<=100ms) não trazem texto novo
    cont=[(i,f,l) for i,f,l in bs if f-i>100 and l]
    duasLinhas=sum(1 for _,_,l in cont if len(l)>=2)
    formato = "duas linhas + transições" if duasLinhas > len(cont)*0.5 else "uma linha, blocos sobrepostos"

    janelas=[]   # janela em que o texto NOVO é realmente falado
    tetos=[]     # o `fim` do bloco, para comparar
    for k,(i,f,l) in enumerate(cont):
        prox = cont[k+1][0] if k+1 < len(cont) else f
        # a fala nova termina quando o próximo texto novo aparece, ou no fim do bloco
        janela = max(0, min(f, prox) - i)
        if janela>0:
            janelas.append(janela); tetos.append(f-i)
    print(f"--- {path}")
    print(f"  formato: {formato}  ({len(cont)} blocos de conteúdo, {100*duasLinhas//max(1,len(cont))}% com 2 linhas)")
    print(f"  janela REAL da fala nova: média {st.mean(janelas)/1000:.2f}s  mediana {st.median(janelas)/1000:.2f}s  p90 {sorted(janelas)[int(.9*len(janelas))]/1000:.2f}s")
    print(f"  `fim` do bloco (ingênuo):  média {st.mean(tetos)/1000:.2f}s  mediana {st.median(tetos)/1000:.2f}s")
    print(f"  ERRO atual (todo texto recebe o início):")
    print(f"    última palavra: ~{st.mean(janelas)/1000:.2f}s  |  palavra média: ~{st.mean(janelas)/2000:.2f}s")
