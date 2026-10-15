n=int(input()); a=sorted(tuple(map(int,input().split())) for _ in range(n)); out=[]
for l,r in a:
    if not out or l>out[-1][1]: out.append([l,r])
    else: out[-1][1]=max(out[-1][1],r)
print(len(out))
for l,r in out: print(l,r)
