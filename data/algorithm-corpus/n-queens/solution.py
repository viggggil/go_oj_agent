n=int(input()); cols=set(); diag1=set(); diag2=set()
def dfs(r):
    if r==n: return 1
    total=0
    for c in range(n):
        if c in cols or r-c in diag1 or r+c in diag2: continue
        cols.add(c); diag1.add(r-c); diag2.add(r+c); total+=dfs(r+1)
        cols.remove(c); diag1.remove(r-c); diag2.remove(r+c)
    return total
print(dfs(0))
