s=input().strip(); st=[]; pairs={')':'(',']':'[','}':'{'}; ok=True
for c in s:
    if c in '([{': st.append(c)
    elif not st or st.pop()!=pairs[c]: ok=False; break
print('YES' if ok and not st else 'NO')
