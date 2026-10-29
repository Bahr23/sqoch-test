#!/usr/bin/env python3
"""Isolated Docker/RabbitMQ acceptance check. Uses only Python's standard library."""

import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
EVENT = "b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1"
EXPECTED_EMAILS = {f"{name}.demo@example.com" for name in
                   ("anna", "alex", "olga", "family", "irina", "sergey")}


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def compose_command():
    if shutil.which("docker") and subprocess.run(
        ["docker", "compose", "version"], stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL, check=False,
    ).returncode == 0:
        return ["docker", "compose"]
    if shutil.which("docker-compose"):
        return ["docker-compose"]
    raise RuntimeError("Нужны Docker и Docker Compose v2")


class Demo:
    def __init__(self, args):
        self.args = args
        self.project = "sqoch-mailing-demo-" + secrets.token_hex(4)
        self.directory = Path(tempfile.mkdtemp(prefix=self.project + "-"))
        self.env_file = self.directory / ".env"
        self.log_file = self.directory / "compose.log"
        self.report = {"project": self.project, "checks": []}
        self.values = dict(line.split("=", 1) for line in
                           (ROOT / ".env.example").read_text().splitlines()
                           if line and not line.startswith("#"))
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", args.port))
            self.port = sock.getsockname()[1]
        self.values.update(
            APP_PORT=str(self.port), ADMIN_HOST="admin.localhost",
            ADMIN_ORIGIN=f"http://admin.localhost:{self.port}",
            ADMIN_EMAIL="demo@sqoch.local", ADMIN_PASSWORD="mailing-demo-password",
            JWT_SECRET=secrets.token_hex(32), NOTIFICATION_SERVICE_TOKEN=secrets.token_hex(32),
            OPENROUTER_API_KEY="", OPENROUTER_MODEL="", DEMO_MODE="true",
            OUTBOX_INTERVAL="50ms", NOTIFIER_INTERVAL="50ms", NOTIFIER_TIMEOUT="2s",
            NOTIFIER_LEASE="8s", NOTIFIER_RETRY_BASE="200ms", NOTIFIER_MAX_ATTEMPTS="3",
            MOCK_SEND_DELAY="0s", MOCK_FAILURE_MODE="always_fail",
        )
        self.write_env()
        # Prevent automatic restart from hiding the deliberate SIGKILL.
        override = self.directory / "compose.demo.json"
        override.write_text(json.dumps({"services": {"notifier": {"restart": "no"}}}))
        self.command = compose_command() + ["-p", self.project, "--env-file", str(self.env_file),
                                           "-f", str(ROOT / "compose.yaml"), "-f", str(override)]
        # Shell variables must not override this isolated demo's .env.
        self.environment = {k: v for k, v in os.environ.items()
                            if k not in self.values and not k.startswith("COMPOSE_")}
        self.http = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
        )

    def write_env(self):
        self.env_file.write_text("".join(f"{k}={v}\n" for k, v in self.values.items()))
        self.env_file.chmod(0o600)

    def compose(self, *arguments, timeout=180):
        with self.log_file.open("a") as log:
            log.write("\n$ " + shlex.join(self.command + list(arguments)) + "\n")
            log.flush()
            subprocess.run(self.command + list(arguments), env=self.environment, cwd=ROOT,
                           stdout=log, stderr=subprocess.STDOUT, timeout=timeout, check=True)

    def api(self, path, data=None):
        body = None if data is None else json.dumps(data).encode()
        request = urllib.request.Request(f"http://127.0.0.1:{self.port}/api/admin/{path}", data=body,
                                         headers={"Host": f"admin.localhost:{self.port}",
                                                  "Origin": self.values["ADMIN_ORIGIN"],
                                                  "Content-Type": "application/json"})
        with self.http.open(request, timeout=10) as response:
            return json.load(response)

    def wait(self, path, predicate, description, timeout=90):
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            try:
                last = self.api(path)
                if predicate(last):
                    return last
            except (urllib.error.URLError, TimeoutError) as error:
                last = str(error)
            time.sleep(0.1)
        raise RuntimeError(f"Таймаут: {description}. Последний ответ: {last}")

    def passed(self, description):
        self.report["checks"].append(description)
        print("OK: " + description, flush=True)

    def mailing(self, body):
        draft = self.api("mailings", {"event_id": EVENT, "body": body})
        path = "mailings/" + draft["id"]
        check(draft["status"] == "draft", "Черновик неожиданно запущен")
        check(not draft.get("deliveries"), "Черновик создал задания до подтверждения")
        return path, {"expected_updated_at": draft["updated_at"], "audience_hash": draft["audience_hash"]}

    def failure_mode(self, mode, delay):
        self.values.update(MOCK_FAILURE_MODE=mode, MOCK_SEND_DELAY=delay)
        self.write_env()
        self.compose("up", "-d", "--no-deps", "--wait", "notifier")

    def run(self):
        print(f"Изолированный стенд: {self.project}\nЛоги: {self.log_file}", flush=True)
        print("Собираем и запускаем четыре сервиса…", flush=True)
        self.compose("up", "-d", "--build", "--wait", "--wait-timeout", "180", timeout=900)
        self.compose("exec", "-T", "backend", "/app/api", "seed-orders")
        self.api("login", {"email": self.values["ADMIN_EMAIL"], "password": self.values["ADMIN_PASSWORD"]})
        people = self.api(f"events/{EVENT}/recipients")
        check(len(people) == 6 and {p["email"] for p in people} == EXPECTED_EMAILS,
              "Неверная аудитория демонстрационной рассылки")
        check(next(p for p in people if p["email"] == "anna.demo@example.com")["order_count"] == 2,
              "Несколько активных покупок не объединены по email")
        check(not any(name in {"София Смирнова", "Виктор Соколов"}
                      for p in people for name in p["names"]), "В аудиторию попала отменённая покупка")
        self.passed("20 покупок: 6 уникальных получателей выбранного ивента, отменённые участники исключены")

        self.compose("stop", "notifier")
        path, confirmation = self.mailing("Демонстрация сбоя: встреча переносится на 19:00.")
        first = self.api(path + "/confirm", confirmation)
        repeated = self.api(path + "/confirm", confirmation)
        ids = {d["id"] for d in first["deliveries"]}
        check(len(ids) == 6 and ids == {d["id"] for d in repeated["deliveries"]}, "Повтор confirm создал дубли")
        check(repeated["progress"]["pending"] == 6, "Отправка произошла при остановленном notifier")
        self.passed("Notifier остановлен: два подтверждения создали ровно 6 ожидающих доставок")

        self.compose("start", "notifier")
        failed = self.wait(path, lambda m: m["status"] == "completed_with_errors", "исчерпание попыток")
        check(failed["progress"]["failed"] == 6 and failed["progress"]["sent"] == 0,
              "always_fail не воспроизвёл сбой")
        check(all(d["attempts"] == 3 and d["error"] for d in failed["deliveries"]),
              "Неверное число попыток или отсутствует причина ошибки")
        self.passed("always_fail: 6 ошибок, у каждого адресата 3 попытки и причина сбоя")
        self.report["failed"] = failed
        print(f"Страница сбоя: {self.values['ADMIN_ORIGIN']}/{path}/edit\n"
              "Вход: demo@sqoch.local / mailing-demo-password", flush=True)
        if self.args.pause_on_failure:
            input("Проверьте страницу результатов. Enter — восстановить службу и продолжить: ")

        self.failure_mode("none", "1s")
        self.api(path + "/resume", {})
        self.api(path + "/resume", {})
        partial = self.wait(path, lambda m: 0 < m["progress"]["sent"] < 6 and m["progress"]["processing"] > 0,
                            "частичная отправка перед SIGKILL")
        already_sent = {d["id"]: d["attempts"] for d in partial["deliveries"] if d["status"] == "sent"}
        self.compose("kill", "-s", "SIGKILL", "notifier")
        self.report["before_kill"] = partial
        self.passed(f"SIGKILL notifier во время обработки; уже отправлено {len(already_sent)} из 6")
        self.compose("restart", "backend", "rabbitmq")
        self.compose("up", "-d", "--no-deps", "--wait", "backend", "rabbitmq")
        # New HTTP session models reopening the page after the service restart.
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.api("login", {"email": self.values["ADMIN_EMAIL"], "password": self.values["ADMIN_PASSWORD"]})
        restored = self.api(path)
        check(ids == {d["id"] for d in restored["deliveries"]}, "Перезапуск потерял исходные доставки")
        self.compose("start", "notifier")
        completed = self.wait(path, lambda m: m["status"] == "completed", "восстановление после SIGKILL")
        check(completed["progress"]["sent"] == 6, "Не все отправки восстановлены")
        for d in completed["deliveries"]:
            if d["id"] in already_sent:
                check(d["attempts"] == already_sent[d["id"]], "Успешная доставка обработана заново")
        for action, payload in (("confirm", confirmation), ("resume", {})):
            repeated = self.api(path + "/" + action, payload)
            check(repeated["deliveries"] == completed["deliveries"], "Повтор после завершения изменил результаты")
        self.report["completed"] = completed
        self.passed("После перезапуска backend/RabbitMQ и продолжения: 6/6 sent; успешные попытки не повторены")

        self.failure_mode("after_accept_once", "0s")
        second_path, confirmation = self.mailing("Отдельная рассылка: проверка потерянного ответа отправителя.")
        self.api(second_path + "/confirm", confirmation)
        second = self.wait(second_path, lambda m: m["status"] == "completed", "потерянный ответ отправителя")
        second_ids = {d["id"] for d in second["deliveries"]}
        check(second["progress"]["sent"] == 6 and not ids.intersection(second_ids),
              "Разные рассылки используют общий ключ отправки")
        self.report["after_accept_once"] = second

        # Clean shutdown checkpoints WAL before copying all three databases.
        self.compose("stop", "notifier", "backend")
        for service, source, name in (("notifier", "/data/notifier.db", "jobs.db"),
                                      ("notifier", "/data/mock-provider.db", "provider.db"),
                                      ("backend", "/data/sqoch.db", "backend.db")):
            self.compose("cp", f"{service}:{source}", str(self.directory / name))
        with sqlite3.connect(self.directory / "provider.db") as db:
            accepted = dict(db.execute("SELECT id,email FROM mock_acceptances"))
            calls = dict(db.execute("SELECT id,calls FROM mock_calls"))
        check(set(accepted) == ids | second_ids, "Приняты лишние письма или потерян результат")
        check(all(calls[i] == 4 for i in ids), "Первая рассылка: ожидались 3 отказа и ровно 1 принятие")
        check(all(calls[i] == 1 for i in second_ids), "Потерянный ответ вызвал повторную отправку")
        with sqlite3.connect(self.directory / "jobs.db") as db:
            check(db.execute("SELECT count(*) FROM notification_jobs WHERE status='sent'").fetchone()[0] == 12,
                  "Число успешно обработанных заданий не совпало")
        with sqlite3.connect(self.directory / "backend.db") as db:
            check(db.execute("SELECT count(*) FROM mailing_deliveries").fetchone()[0] == 12, "Дубли доставок в backend")
            check(db.execute("SELECT count(*) FROM message_outbox").fetchone()[0] == 18, "Confirm/resume создали лишние команды")
        self.report["provider_acceptances"] = len(accepted)
        self.report["provider_calls"] = calls
        self.passed("Сверка SQLite: 12 принятых писем для двух рассылок, по одному на email в каждой; дублей нет")
        self.passed("after_accept_once: потерянные ответы восстановлены без второго вызова Send")
        if self.args.keep:
            self.failure_mode("none", "1s")
            self.compose("up", "-d", "--no-deps", "--wait", "backend")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=0, help="порт frontend; по умолчанию свободный")
    parser.add_argument("--pause-on-failure", action="store_true", help="пауза для просмотра сбоя в админке")
    parser.add_argument("--keep", action="store_true", help="оставить отдельный стенд после проверки")
    args = parser.parse_args()
    demo = Demo(args)
    try:
        demo.run()
        demo.report["result"] = "passed"
    except (Exception, KeyboardInterrupt) as error:
        demo.report.update(result="failed", error=str(error))
        raise
    finally:
        report = demo.directory / "report.json"
        report.write_text(json.dumps(demo.report, ensure_ascii=False, indent=2) + "\n")
        print(f"Отчёт: {report}", flush=True)
        if args.keep:
            print("Стенд сохранён. Удалить только его: " + shlex.join(demo.command + ["down", "-v"]), flush=True)
        else:
            # Unique generated project: never touches the user's normal stack or volumes.
            demo.compose("down", "-v", "--remove-orphans")


if __name__ == "__main__":
    main()
