# audit

`audit` adalah package Go untuk membuat **audit trail** lintas database (MySQL & PostgreSQL).  
Audit trail akan disimpan di database terpisah, dan jika tabel audit belum ada, package ini akan otomatis membuatnya sesuai struktur tabel aslinya.

## ✨ Fitur
- **Multi Database Driver** → MySQL & PostgreSQL
- **Auto Create Table** → Membuat tabel audit jika belum ada
- **Async Worker** → Penyimpanan audit tidak menghambat request utama
- **Dynamic Snapshot** → Menyimpan kondisi data saat operasi terjadi (`INSERT`, `UPDATE`, `DELETE`)

## 📦 Instalasi
```bash
go get github.com/rezachris20/go-audit