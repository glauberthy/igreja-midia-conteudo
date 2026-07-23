package main

import (
	"testing"
	"time"

	"srtclean/internal/pipeline"
)

func ped(url, inicio, fim string) *pipeline.Pedido {
	return pipeline.NovoPedido("sermao", url, inicio, fim, time.Unix(0, 0).UTC())
}

func TestDecidirDownloadSemPedidoAnterior(t *testing.T) {
	if d := decidirDownload(nil, ped("u", "00:00:00", "00:10:00"), false); d != baixarNormal {
		t.Errorf("id novo deveria baixar normal, veio %v", d)
	}
}

func TestDecidirDownloadMesmoPedido(t *testing.T) {
	e := ped("u", "00:00:00", "00:10:00")
	n := ped("u", "00:00:00", "00:10:00")
	if d := decidirDownload(e, n, false); d != baixarNormal {
		t.Errorf("mesmo pedido deveria baixar normal (idempotente), veio %v", d)
	}
}

func TestDecidirDownloadUrlDiferenteRecusa(t *testing.T) {
	e := ped("antigo", "00:00:00", "00:10:00")
	n := ped("novo", "00:00:00", "00:10:00")
	if d := decidirDownload(e, n, false); d != recusarConflito {
		t.Errorf("URL diferente sem -force deveria RECUSAR (não misturar vídeo), veio %v", d)
	}
}

func TestDecidirDownloadJanelaDiferenteRecusa(t *testing.T) {
	e := ped("u", "00:00:00", "00:10:00")
	n := ped("u", "00:05:00", "00:15:00") // mesma URL, janela diferente
	if d := decidirDownload(e, n, false); d != recusarConflito {
		t.Errorf("janela diferente sem -force deveria recusar, veio %v", d)
	}
}

func TestDecidirDownloadForceSubstitui(t *testing.T) {
	e := ped("antigo", "00:00:00", "00:10:00")
	n := ped("novo", "00:00:00", "00:10:00")
	if d := decidirDownload(e, n, true); d != limparERebaixar {
		t.Errorf("URL diferente com -force deveria limpar e rebaixar, veio %v", d)
	}
}

func TestDecidirDownloadForceMesmoPedidoRebaixa(t *testing.T) {
	e := ped("u", "00:00:00", "00:10:00")
	n := ped("u", "00:00:00", "00:10:00")
	if d := decidirDownload(e, n, true); d != limparERebaixar {
		t.Errorf("-force no mesmo pedido deveria limpar e rebaixar do zero, veio %v", d)
	}
}
