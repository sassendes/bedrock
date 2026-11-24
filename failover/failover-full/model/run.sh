#!/usr/bin/env bash
set -u
cd "$(dirname "$0")"

if ! command -v java >/dev/null; then
  if command -v nix-shell >/dev/null && [ -z "${IN_RUN_SHELL:-}" ]; then
    exec env IN_RUN_SHELL=1 SKIP="${SKIP:-}" nix-shell -p jdk curl --run "./run.sh ${1:-}"
  fi
  echo "java missing and no nix-shell to get it"
  exit 1
fi

JAR=tla2tools.jar
jar_ok() { [ -f "$JAR" ] && jar tf "$JAR" 2>/dev/null | grep -q "^tlc2/TLC.class$"; }
if ! jar_ok; then
  rm -f "$JAR" "$JAR.part"
  echo "downloading $JAR"
  curl -sSfL -o "$JAR.part" https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar && mv "$JAR.part" "$JAR"
  jar_ok || { rm -f "$JAR" "$JAR.part"; echo "$JAR is broken or could not be downloaded"; exit 1; }
fi

mods=$(ls MC*.tla | sed 's/^MC//; s/\.tla$//' | awk '{ print length, $0 }' | sort -rn | cut -d' ' -f2)
only="${1:-}"
ok=0; bad=0; wrong=()

for cfg in *.cfg; do
  name="${cfg%.cfg}"
  [ -n "$only" ] && [[ "$name" != "$only"* ]] && continue
  [[ " ${SKIP:-} " == *" $name "* ]] && { echo "skip  $name"; continue; }
  mod=""
  for m in $mods; do
    if [[ "$name" == "$m"* ]]; then mod="$m"; break; fi
  done
  [ -z "$mod" ] && { echo "?? $cfg has no model"; continue; }
  case "$name" in
    *Safety|*Liveness|HaltCheckerOnly|HaltStaleOnly) want=pass ;;
    *) want=fail ;;
  esac
  dir=$(mktemp -d)
  log="$dir/out.txt"
  java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC -workers auto -deadlock -metadir "$dir/meta" -config "$cfg" "MC$mod.tla" > "$log" 2>&1 &
  pid=$!
  trap 'kill "$pid" 2>/dev/null; rm -rf "$dir"; echo; exit 130' INT
  start=$SECONDS
  while kill -0 "$pid" 2>/dev/null; do
    el=$((SECONDS - start))
    prog=$(grep -E "^Progress|states generated" "$log" | tail -1 | grep -oE "[0-9,]+ states generated.*" | cut -c1-90)
    [ -z "$prog" ] && prog=$(tail -1 "$log" | cut -c1-60)
    printf "\r..    %-40s %3dm%02ds  %-90s" "$name" $((el/60)) $((el%60)) "$prog"
    sleep 3
  done
  wait "$pid"
  printf "\r%-150s\r" ""
  out=$(cat "$log")
  rm -rf "$dir"
  if echo "$out" | grep -q "No error has been found"; then got=pass
  elif echo "$out" | grep -qE "(Invariant|Action property|Temporal property) [A-Za-z0-9_]+ (is|was) violated|Temporal properties were violated"; then got=fail
  else got=broken
  fi
  states=$(echo "$out" | grep -oE "[0-9,]+ distinct states found" | head -1)
  if [ "$got" = "$want" ]; then
    ok=$((ok+1)); printf "ok    %-40s %-6s %4ds  %s\n" "$name" "$got" $((SECONDS - start)) "$states"
  else
    bad=$((bad+1)); wrong+=("$name"); printf "WRONG %-40s want %-5s got %-6s %s\n" "$name" "$want" "$got" "$states"
    [ "$got" = "broken" ] && echo "$out" | grep -vE "^(Picked up|Linting|Semantic|Parsing|TLC2|Running|Starting)" | head -4 | sed 's/^/        /'
    echo "$out" > "result-$name.txt"
  fi
done

echo
echo "$ok ok  $bad wrong"
[ "$bad" -gt 0 ] && echo "full output of every wrong one is in result-<name>.txt"
